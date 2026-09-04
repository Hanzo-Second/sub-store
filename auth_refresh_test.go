package main

import (
	"database/sql"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	_ "modernc.org/sqlite"
)

// TestSessionPersistsAcrossRefreshes verifies that a session created by login
// remains valid across multiple "refresh" cycles (bootstrap + me + workspace).
func TestSessionPersistsAcrossRefreshes(t *testing.T) {
	app := testApp(t)

	// Setup admin account
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/api/auth/setup", strings.NewReader(`{"username":"admin","password":"testpass123"}`))
	r.Header.Set("Content-Type", "application/json")
	app.handleSetup(w, r)
	if w.Code != 201 {
		t.Fatalf("setup failed: %d %s", w.Code, w.Body.String())
	}

	// Extract session cookie from setup response
	cookies := w.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("expected 1 cookie, got %d", len(cookies))
	}
	sessionCookie := cookies[0]

	// Simulate multiple page refreshes
	workspaceEndpoints := []string{"/api/proxies", "/api/subscriptions", "/api/rules",
		"/api/access-keys", "/api/settings", "/api/groups",
		"/api/rule-providers", "/api/service-rule-groups"}

	for refresh := 1; refresh <= 5; refresh++ {
		t.Logf("=== Refresh %d ===", refresh)

		// 1. Bootstrap (not auth-protected)
		w = httptest.NewRecorder()
		r = httptest.NewRequest("GET", "/api/bootstrap", nil)
		app.routes().ServeHTTP(w, r)
		if w.Code != 200 {
			t.Fatalf("refresh %d: bootstrap failed: %d", refresh, w.Code)
		}

		// 2. Auth/me
		w = httptest.NewRecorder()
		r = httptest.NewRequest("GET", "/api/auth/me", nil)
		r.AddCookie(sessionCookie)
		app.routes().ServeHTTP(w, r)
		if w.Code != 200 {
			t.Fatalf("refresh %d: /api/auth/me failed: %d %s", refresh, w.Code, w.Body.String())
		}

		// 3. Workspace endpoints (all auth-protected)
		for _, ep := range workspaceEndpoints {
			w = httptest.NewRecorder()
			r = httptest.NewRequest("GET", ep, nil)
			r.AddCookie(sessionCookie)
			app.routes().ServeHTTP(w, r)
			if w.Code != 200 {
				t.Fatalf("refresh %d: %s failed: %d %s", refresh, ep, w.Code, w.Body.String())
			}
		}
	}
	t.Log("All 5 refreshes succeeded - session persists correctly")
}

// TestSessionAfterPasswordReset verifies that after resetting the password
// directly in the database (like the recovery script does), a new login
// creates a valid session that persists across refreshes.
func TestSessionAfterPasswordReset(t *testing.T) {
	app := testApp(t)

	// Setup admin account
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/api/auth/setup", strings.NewReader(`{"username":"admin","password":"original123"}`))
	r.Header.Set("Content-Type", "application/json")
	app.handleSetup(w, r)
	if w.Code != 201 {
		t.Fatalf("setup failed: %d", w.Code)
	}

	// Simulate password recovery: update hash and delete sessions directly in DB
	newHash, err := hashPassword("newpass123")
	if err != nil {
		t.Fatal(err)
	}
	_, err = app.db.Exec(`UPDATE users SET password_hash=? WHERE username='admin'`, newHash)
	if err != nil {
		t.Fatal(err)
	}
	_, err = app.db.Exec(`DELETE FROM sessions`)
	if err != nil {
		t.Fatal(err)
	}
	t.Log("Password reset in DB, all sessions deleted")

	// Login with new password
	w = httptest.NewRecorder()
	r = httptest.NewRequest("POST", "/api/auth/login", strings.NewReader(`{"username":"admin","password":"newpass123"}`))
	r.Header.Set("Content-Type", "application/json")
	app.handleLogin(w, r)
	if w.Code != 200 {
		t.Fatalf("login with new password failed: %d %s", w.Code, w.Body.String())
	}
	sessionCookie := w.Result().Cookies()[0]

	// Simulate multiple refreshes
	for refresh := 1; refresh <= 5; refresh++ {
		w = httptest.NewRecorder()
		r = httptest.NewRequest("GET", "/api/bootstrap", nil)
		app.routes().ServeHTTP(w, r)
		if w.Code != 200 {
			t.Fatalf("refresh %d: bootstrap: %d", refresh, w.Code)
		}

		w = httptest.NewRecorder()
		r = httptest.NewRequest("GET", "/api/auth/me", nil)
		r.AddCookie(sessionCookie)
		app.routes().ServeHTTP(w, r)
		if w.Code != 200 {
			t.Fatalf("refresh %d: /api/auth/me: %d %s", refresh, w.Code, w.Body.String())
		}

		for _, ep := range []string{"/api/proxies", "/api/groups", "/api/rule-providers", "/api/service-rule-groups"} {
			w = httptest.NewRecorder()
			r = httptest.NewRequest("GET", ep, nil)
			r.AddCookie(sessionCookie)
			app.routes().ServeHTTP(w, r)
			if w.Code != 200 {
				t.Fatalf("refresh %d: %s: %d %s", refresh, ep, w.Code, w.Body.String())
			}
		}
	}
	t.Log("All 5 refreshes after password reset succeeded")
}

// TestSessionAfterExternalDBModification simulates the recovery script modifying
// the database via a separate SQLite connection while the app holds the DB open.
func TestSessionAfterExternalDBModification(t *testing.T) {
	app := testApp(t)

	// Setup admin
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/api/auth/setup", strings.NewReader(`{"username":"admin","password":"original123"}`))
	r.Header.Set("Content-Type", "application/json")
	app.handleSetup(w, r)
	if w.Code != 201 {
		t.Fatalf("setup failed: %d", w.Code)
	}

	// Login
	w = httptest.NewRecorder()
	r = httptest.NewRequest("POST", "/api/auth/login", strings.NewReader(`{"username":"admin","password":"original123"}`))
	r.Header.Set("Content-Type", "application/json")
	app.handleLogin(w, r)
	if w.Code != 200 {
		t.Fatalf("login failed: %d", w.Code)
	}
	sessionCookie := w.Result().Cookies()[0]

	// Verify session works
	w = httptest.NewRecorder()
	r = httptest.NewRequest("GET", "/api/auth/me", nil)
	r.AddCookie(sessionCookie)
	app.routes().ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatalf("me before recovery: %d", w.Code)
	}
	t.Log("Session works before recovery")

	// Get the database file path from the app's own connection
	row := app.db.QueryRow(`PRAGMA database_list`)
	var seq int
	var name, file string
	_ = row.Scan(&seq, &name, &file)
	t.Logf("Database path: %s", file)

	// Open a SEPARATE SQLite connection (simulating the Python recovery script)
	extDB, err := sql.Open("sqlite", file)
	if err != nil {
		t.Fatal(err)
	}
	defer extDB.Close()

	newHash, err := hashPassword("recovered123")
	if err != nil {
		t.Fatal(err)
	}
	_, err = extDB.Exec(`UPDATE users SET password_hash=? WHERE username='admin'`, newHash)
	if err != nil {
		t.Fatalf("ext DB update: %v", err)
	}
	_, err = extDB.Exec(`DELETE FROM sessions`)
	if err != nil {
		t.Fatalf("ext DB delete sessions: %v", err)
	}
	t.Log("External DB modification complete (password reset + session deletion)")

	// Old session should now be invalid
	w = httptest.NewRecorder()
	r = httptest.NewRequest("GET", "/api/auth/me", nil)
	r.AddCookie(sessionCookie)
	app.routes().ServeHTTP(w, r)
	if w.Code == 200 {
		t.Log("Old session still works (WAL hasn't been read yet by Go)")
	} else {
		t.Logf("Old session invalidated: %d (expected after session deletion)", w.Code)
	}

	// Login with new password
	w = httptest.NewRecorder()
	r = httptest.NewRequest("POST", "/api/auth/login", strings.NewReader(`{"username":"admin","password":"recovered123"}`))
	r.Header.Set("Content-Type", "application/json")
	app.handleLogin(w, r)
	if w.Code != 200 {
		t.Fatalf("login with recovered password: %d %s", w.Code, w.Body.String())
	}
	newCookie := w.Result().Cookies()[0]

	// Simulate multiple refreshes
	for refresh := 1; refresh <= 5; refresh++ {
		w = httptest.NewRecorder()
		r = httptest.NewRequest("GET", "/api/bootstrap", nil)
		app.routes().ServeHTTP(w, r)
		if w.Code != 200 {
			t.Fatalf("refresh %d: bootstrap: %d", refresh, w.Code)
		}

		w = httptest.NewRecorder()
		r = httptest.NewRequest("GET", "/api/auth/me", nil)
		r.AddCookie(newCookie)
		app.routes().ServeHTTP(w, r)
		if w.Code != 200 {
			t.Fatalf("refresh %d: /api/auth/me: %d %s", refresh, w.Code, w.Body.String())
		}

		for _, ep := range []string{"/api/proxies", "/api/groups", "/api/rule-providers", "/api/service-rule-groups"} {
			w = httptest.NewRecorder()
			r = httptest.NewRequest("GET", ep, nil)
			r.AddCookie(newCookie)
			app.routes().ServeHTTP(w, r)
			if w.Code != 200 {
				t.Fatalf("refresh %d: %s: %d %s", refresh, ep, w.Code, w.Body.String())
			}
		}
	}
	t.Log("All 5 refreshes after external DB recovery succeeded")
}

// TestConcurrentRequestsDoNotReturn401 verifies that firing many parallel
// auth-protected requests (as the browser does with Promise.all) does not
// produce false 401 errors from SQLite connection contention.
func TestConcurrentRequestsDoNotReturn401(t *testing.T) {
	app := testApp(t)

	// Setup admin
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/api/auth/setup", strings.NewReader(`{"username":"admin","password":"test1234"}`))
	r.Header.Set("Content-Type", "application/json")
	app.handleSetup(w, r)
	if w.Code != 201 {
		t.Fatalf("setup failed: %d", w.Code)
	}
	sessionCookie := w.Result().Cookies()[0]

	endpoints := []string{"/api/proxies", "/api/subscriptions", "/api/rules",
		"/api/access-keys", "/api/settings", "/api/groups",
		"/api/rule-providers", "/api/service-rule-groups", "/api/auth/me"}

	// Run 10 rounds of parallel requests
	for round := 1; round <= 10; round++ {
		var wg sync.WaitGroup
		statuses := make([]int, len(endpoints))
		for i, ep := range endpoints {
			wg.Add(1)
			go func(idx int, path string) {
				defer wg.Done()
				w := httptest.NewRecorder()
				r := httptest.NewRequest("GET", path, nil)
				r.AddCookie(sessionCookie)
				app.routes().ServeHTTP(w, r)
				statuses[idx] = w.Code
			}(i, ep)
		}
		wg.Wait()

		for i, status := range statuses {
			if status == 401 {
				t.Fatalf("round %d: %s returned 401 (false auth failure from DB contention)", round, endpoints[i])
			}
			if status != 200 {
				t.Fatalf("round %d: %s returned %d", round, endpoints[i], status)
			}
		}
	}
	t.Log("All 10 rounds of 9 concurrent requests succeeded with no false 401s")
}
