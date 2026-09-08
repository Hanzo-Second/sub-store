package main

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"math"
	"mime"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
	_ "modernc.org/sqlite"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

func testApp(t *testing.T) *App {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "substore.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	app := &App{db: db}
	if err := app.migrate(); err != nil {
		t.Fatal(err)
	}
	return app
}

func TestFrontendAssetsAreEmbedded(t *testing.T) {
	webRoot, err := fs.Sub(webAssets, "web")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"index.html", "styles.css", "app.js", "favicon.svg"} {
		contents, err := fs.ReadFile(webRoot, name)
		if err != nil {
			t.Fatalf("embedded %s: %v", name, err)
		}
		if len(contents) == 0 {
			t.Fatalf("embedded %s is empty", name)
		}
	}
}

func TestAccountUpdateRequiresCurrentPasswordAndSignsOutOtherSessions(t *testing.T) {
	app := testApp(t)
	oldHash, err := hashPassword("old-password")
	if err != nil {
		t.Fatal(err)
	}
	result, err := app.db.Exec(`INSERT INTO users(username,password_hash,role,enabled,created_at) VALUES('admin',?,'admin',1,'now')`, oldHash)
	if err != nil {
		t.Fatal(err)
	}
	userID, _ := result.LastInsertId()
	if _, err := app.db.Exec(`INSERT INTO sessions(id,user_id,expires_at,created_at) VALUES('current',?,'2099-01-01T00:00:00Z','now'),('other',?,'2099-01-01T00:00:00Z','now')`, userID, userID); err != nil {
		t.Fatal(err)
	}
	requestBody := `{"username":"renamed","currentPassword":"old-password","newPassword":"new-password","confirmNewPassword":"new-password"}`
	request := httptest.NewRequest(http.MethodPatch, "/api/auth/account", strings.NewReader(requestBody))
	request.AddCookie(&http.Cookie{Name: "substore_session", Value: "current"})
	request = request.WithContext(context.WithValue(request.Context(), userKey, User{ID: userID, Username: "admin", Role: "admin"}))
	recorder := httptest.NewRecorder()
	app.handleAccount(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("account update returned %d: %s", recorder.Code, recorder.Body.String())
	}
	var username, passwordHash string
	if err := app.db.QueryRow(`SELECT username,password_hash FROM users WHERE id=?`, userID).Scan(&username, &passwordHash); err != nil {
		t.Fatal(err)
	}
	if username != "renamed" || verifyPassword("old-password", passwordHash) || !verifyPassword("new-password", passwordHash) {
		t.Fatalf("account credentials were not updated correctly")
	}
	var currentSessions, otherSessions int
	_ = app.db.QueryRow(`SELECT COUNT(*) FROM sessions WHERE id='current'`).Scan(&currentSessions)
	_ = app.db.QueryRow(`SELECT COUNT(*) FROM sessions WHERE id='other'`).Scan(&otherSessions)
	if currentSessions != 1 || otherSessions != 0 {
		t.Fatalf("expected current session to remain and other session to be removed, got current=%d other=%d", currentSessions, otherSessions)
	}
}

func TestAccountUpdateRejectsWrongCurrentPassword(t *testing.T) {
	app := testApp(t)
	passwordHash, _ := hashPassword("old-password")
	result, _ := app.db.Exec(`INSERT INTO users(username,password_hash,role,enabled,created_at) VALUES('admin',?,'admin',1,'now')`, passwordHash)
	userID, _ := result.LastInsertId()
	request := httptest.NewRequest(http.MethodPatch, "/api/auth/account", strings.NewReader(`{"username":"renamed","currentPassword":"wrong-password"}`))
	request = request.WithContext(context.WithValue(request.Context(), userKey, User{ID: userID, Username: "admin", Role: "admin"}))
	recorder := httptest.NewRecorder()
	app.handleAccount(recorder, request)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("wrong current password returned %d, want 401", recorder.Code)
	}
}

func TestDefaultSubscriptionClientIsClashVerge(t *testing.T) {
	app := testApp(t)
	if got := app.settings().DefaultUserAgent; got != "clash-verge/v2.4.5" {
		t.Fatalf("default subscription client = %q, want Clash Verge", got)
	}
}

func TestSubscriptionUsageAndExpiryFromHeader(t *testing.T) {
	usage := parseSubscriptionUserInfo("upload=1073741824; download=2147483648; total=10737418240; expire=1893456000")
	if !usage.HasUsage || !usage.HasTotal || !usage.HasExpire {
		t.Fatalf("expected all subscription usage fields to be detected: %+v", usage)
	}
	if math.Abs(usage.UsedGB-3) > 0.0001 || math.Abs(usage.TotalGB-10) > 0.0001 {
		t.Fatalf("unexpected usage: used=%f total=%f", usage.UsedGB, usage.TotalGB)
	}
	if usage.ExpireAt != "2030-01-01T00:00:00Z" {
		t.Fatalf("unexpected expiry: %s", usage.ExpireAt)
	}
}

func TestSubscriptionUsageHistoryDeduplicatesURLAndHandlesRenewal(t *testing.T) {
	app := testApp(t)
	const rawURL = "https://provider.example/shared-token"
	if _, err := app.db.Exec(`INSERT INTO subscriptions(name,url,path,user_agent,enabled,created_at) VALUES('First',?,'./proxy-providers/first.yaml','clash-meta',1,'now'),('Duplicate',?,'./proxy-providers/duplicate.yaml','clash-meta',1,'now')`, rawURL, rawURL); err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 8, 6, 0, 0, 0, 0, time.UTC)
	record := func(at time.Time, header string) {
		t.Helper()
		if err := app.recordSubscriptionUsage(rawURL, at.Format(time.RFC3339), header, time.Hour); err != nil {
			t.Fatal(err)
		}
	}
	record(start, "upload=0; download=10737418240; total=107374182400; expire=1800000000")
	record(start.Add(30*time.Minute), "upload=0; download=12884901888; total=107374182400; expire=1800000000")
	record(start.Add(time.Hour), "upload=0; download=16106127360; total=107374182400; expire=1800000000")
	record(start.Add(2*time.Hour), "upload=0; download=2147483648; total=214748364800; expire=1900000000")

	var sourceCount, sampleCount, resetCount int
	var tracked float64
	if err := app.db.QueryRow(`SELECT COUNT(*) FROM subscription_usage_sources`).Scan(&sourceCount); err != nil {
		t.Fatal(err)
	}
	if err := app.db.QueryRow(`SELECT COUNT(*),COALESCE(SUM(delta_gb),0),COALESCE(SUM(CASE WHEN event='plan-renewed' THEN 1 ELSE 0 END),0) FROM subscription_usage_samples`).Scan(&sampleCount, &tracked, &resetCount); err != nil {
		t.Fatal(err)
	}
	if sourceCount != 1 || sampleCount != 3 {
		t.Fatalf("expected one shared source and three hourly samples, got sources=%d samples=%d", sourceCount, sampleCount)
	}
	if math.Abs(tracked-7) > 0.0001 || resetCount != 1 {
		t.Fatalf("expected 5 GB growth plus 2 GB after renewal, got tracked=%f resets=%d", tracked, resetCount)
	}
	var firstUsed, duplicateUsed float64
	if err := app.db.QueryRow(`SELECT used_gb FROM subscriptions WHERE name='First'`).Scan(&firstUsed); err != nil {
		t.Fatal(err)
	}
	if err := app.db.QueryRow(`SELECT used_gb FROM subscriptions WHERE name='Duplicate'`).Scan(&duplicateUsed); err != nil {
		t.Fatal(err)
	}
	if firstUsed != 2 || duplicateUsed != 2 {
		t.Fatalf("shared URL subscriptions did not receive the same provider counter: %f %f", firstUsed, duplicateUsed)
	}
	var duplicateID int64
	if err := app.db.QueryRow(`SELECT id FROM subscriptions WHERE name='Duplicate'`).Scan(&duplicateID); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/api/subscriptions/"+strconv.FormatInt(duplicateID, 10)+"/usage", nil)
	recorder := httptest.NewRecorder()
	app.handleSubscriptionAction(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("usage history returned %d: %s", recorder.Code, recorder.Body.String())
	}
	var history subscriptionUsageResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &history); err != nil {
		t.Fatal(err)
	}
	if history.SubscriptionName != "Duplicate" || history.Summary.SampleCount != 3 || math.Abs(history.Summary.TrackedGB-7) > 0.0001 || len(history.Samples) != 3 {
		t.Fatalf("unexpected shared URL usage history: %+v", history)
	}
}

func TestUsageCollectionClaimIsHourlyPerURL(t *testing.T) {
	app := testApp(t)
	start := time.Date(2026, 8, 6, 0, 0, 0, 0, time.UTC)
	due, err := app.claimUsageCollection("https://provider.example/token", start.Format(time.RFC3339))
	if err != nil || !due {
		t.Fatalf("first collection was not due: due=%v err=%v", due, err)
	}
	due, err = app.claimUsageCollection("https://provider.example/token", start.Add(59*time.Minute).Format(time.RFC3339))
	if err != nil || due {
		t.Fatalf("duplicate URL was collected before one hour: due=%v err=%v", due, err)
	}
	due, err = app.claimUsageCollection("https://provider.example/token", start.Add(time.Hour).Format(time.RFC3339))
	if err != nil || !due {
		t.Fatalf("URL was not collectible after one hour: due=%v err=%v", due, err)
	}
}

func TestSubscriptionNamesMustBeUnique(t *testing.T) {
	app := testApp(t)
	create := func(name string) *httptest.ResponseRecorder {
		body, _ := json.Marshal(subscription{Name: name, URL: "https://provider.example/sub", UserAgent: "clash-meta", Enabled: true})
		request := httptest.NewRequest(http.MethodPost, "/api/subscriptions", strings.NewReader(string(body)))
		recorder := httptest.NewRecorder()
		app.handleSubscriptions(recorder, request)
		return recorder
	}
	if recorder := create("Provider"); recorder.Code != http.StatusCreated {
		t.Fatalf("first subscription returned %d: %s", recorder.Code, recorder.Body.String())
	}
	if recorder := create(" provider "); recorder.Code != http.StatusConflict {
		t.Fatalf("duplicate subscription name returned %d, want 409", recorder.Code)
	}
}

func TestDeleteReportsNamedDependencies(t *testing.T) {
	app := testApp(t)
	subResult, err := app.db.Exec(`INSERT INTO subscriptions(name,url,user_agent,enabled,created_at) VALUES('Shared source','https://provider.example/sub','clash-meta',1,'now')`)
	if err != nil {
		t.Fatal(err)
	}
	subscriptionID, _ := subResult.LastInsertId()
	proxyResult, err := app.db.Exec(`INSERT INTO proxies(name,protocol,address,port,credential,enabled,created_at) VALUES('Manual node','ss','proxy.example.com',443,'secret',1,'now')`)
	if err != nil {
		t.Fatal(err)
	}
	proxyID, _ := proxyResult.LastInsertId()
	providerResult, err := app.db.Exec(`INSERT INTO rule_providers(name,primary_url,enabled,created_at) VALUES('Streaming rules','https://rules.example/list',1,'now')`)
	if err != nil {
		t.Fatal(err)
	}
	providerID, _ := providerResult.LastInsertId()
	members, _ := json.Marshal([]string{fmt.Sprintf("subscription-id:%d", subscriptionID), "Manual node"})
	groupResult, err := app.db.Exec(`INSERT INTO proxy_groups(name,group_type,proxies_json,enabled,created_at) VALUES('Primary route','select',?,1,'now')`, string(members))
	if err != nil {
		t.Fatal(err)
	}
	groupID, _ := groupResult.LastInsertId()
	parentMembers, _ := json.Marshal([]string{fmt.Sprintf("group-id:%d", groupID)})
	if _, err := app.db.Exec(`INSERT INTO proxy_groups(name,group_type,proxies_json,enabled,created_at) VALUES('Parent route','select',?,1,'now')`, string(parentMembers)); err != nil {
		t.Fatal(err)
	}
	if _, err := app.db.Exec(`INSERT INTO rules(rule_type,match_value,target,target_group_id,priority,enabled,created_at) VALUES('DOMAIN-SUFFIX','example.com','Primary route',?,10,1,'now'),('RULE-SET','Streaming rules','Primary route',?,20,1,'now')`, groupID, groupID); err != nil {
		t.Fatal(err)
	}
	if _, err := app.db.Exec(`INSERT INTO service_rule_groups(name,target_group_id,enabled,created_at) VALUES('AI traffic',?,1,'now')`, groupID); err != nil {
		t.Fatal(err)
	}

	deleteRecord := func(path string, handler func(http.ResponseWriter, *http.Request)) string {
		t.Helper()
		recorder := httptest.NewRecorder()
		handler(recorder, httptest.NewRequest(http.MethodDelete, path, nil))
		if recorder.Code != http.StatusConflict {
			t.Fatalf("delete %s returned %d, want 409: %s", path, recorder.Code, recorder.Body.String())
		}
		return recorder.Body.String()
	}
	if body := deleteRecord("/api/subscriptions/"+strconv.FormatInt(subscriptionID, 10), app.handleSubscriptionAction); !strings.Contains(body, `proxy group \"Primary route\"`) {
		t.Fatalf("subscription dependency was not named: %s", body)
	}
	if body := deleteRecord("/api/proxies/"+strconv.FormatInt(proxyID, 10), app.handleProxyAction); !strings.Contains(body, `proxy group \"Primary route\"`) {
		t.Fatalf("proxy dependency was not named: %s", body)
	}
	groupBody := deleteRecord("/api/groups/"+strconv.FormatInt(groupID, 10), app.handleGroupAction)
	for _, dependency := range []string{`proxy group \"Parent route\"`, `routing rule \"DOMAIN-SUFFIX example.com\"`, `service rule group \"AI traffic\"`} {
		if !strings.Contains(groupBody, dependency) {
			t.Fatalf("proxy group dependency %q was not named: %s", dependency, groupBody)
		}
	}
	if body := deleteRecord("/api/rule-providers/"+strconv.FormatInt(providerID, 10), app.handleRuleProviderAction); !strings.Contains(body, `routing rule \"RULE-SET Streaming rules → Primary route\"`) {
		t.Fatalf("provider dependency was not named: %s", body)
	}
}

func TestProxyGroupRejectsDuplicateMembers(t *testing.T) {
	app := testApp(t)
	if err := app.validateGroupMembers("Duplicate group", []string{"DIRECT", "DIRECT"}, 0); err == nil || !strings.Contains(err.Error(), "only be added once") {
		t.Fatalf("duplicate group members were accepted: %v", err)
	}
}

func TestSubscriptionPathAndUsageAreManagedAutomatically(t *testing.T) {
	app := testApp(t)
	createBody, _ := json.Marshal(subscription{Name: "Provider One", URL: "https://provider.example/sub", Path: "./custom.yaml", UserAgent: "clash-meta", Enabled: true, UsedGB: 98, TotalGB: 100})
	createRequest := httptest.NewRequest(http.MethodPost, "/api/subscriptions", strings.NewReader(string(createBody)))
	createRecorder := httptest.NewRecorder()
	app.handleSubscriptions(createRecorder, createRequest)
	if createRecorder.Code != http.StatusCreated {
		t.Fatalf("create returned %d: %s", createRecorder.Code, createRecorder.Body.String())
	}
	items := app.listSubscriptions()
	if len(items) != 1 || items[0].Path != "./proxy-providers/provider-one.yaml" {
		t.Fatalf("expected automatic subscription path, got %+v", items)
	}
	if items[0].UsedGB != 0 || items[0].TotalGB != 0 {
		t.Fatalf("create accepted manual usage: %+v", items[0])
	}
	if _, err := app.db.Exec(`UPDATE subscriptions SET used_gb=12.5,total_gb=50 WHERE id=?`, items[0].ID); err != nil {
		t.Fatal(err)
	}
	patchBody, _ := json.Marshal(subscription{Name: "Provider Renamed", URL: "https://provider.example/sub", Path: "./another-custom.yaml", UserAgent: "clash-meta", Enabled: true, UpdateMode: "manual", IntervalMinutes: 1440, UsedGB: 1, TotalGB: 2})
	patchRequest := httptest.NewRequest(http.MethodPatch, "/api/subscriptions/"+strconv.FormatInt(items[0].ID, 10), strings.NewReader(string(patchBody)))
	patchRecorder := httptest.NewRecorder()
	app.handleSubscriptionAction(patchRecorder, patchRequest)
	if patchRecorder.Code != http.StatusOK {
		t.Fatalf("patch returned %d: %s", patchRecorder.Code, patchRecorder.Body.String())
	}
	updated := app.listSubscriptions()[0]
	if updated.Path != "./proxy-providers/provider-renamed.yaml" {
		t.Fatalf("rename did not regenerate subscription path: %s", updated.Path)
	}
	if updated.UsedGB != 12.5 || updated.TotalGB != 50 {
		t.Fatalf("patch overwrote provider usage: %+v", updated)
	}
}

func TestAccessKeyPublishesMonthlyAllowanceAndSubStoreName(t *testing.T) {
	app := testApp(t)
	key := "monthly-test-key"
	if _, err := app.db.Exec(`INSERT INTO access_keys(name,key_hash,key_value,key_preview,enabled,monthly_data_gb,created_at) VALUES(?,?,?,?,1,?,?)`, "家用 Laptop", hashToken(key), key, "mo••ey", 25.5, time.Now().UTC().Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/sub/"+key, nil)
	recorder := httptest.NewRecorder()
	app.handlePublicSubscription(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("subscription returned %d", recorder.Code)
	}
	header := recorder.Header().Get("Subscription-Userinfo")
	usage := parseSubscriptionUserInfo(header)
	if !usage.HasTotal || math.Abs(usage.TotalGB-25.5) > 0.0001 {
		t.Fatalf("unexpected per-key allowance header %q", header)
	}
	profileTitle := recorder.Header().Get("Profile-Title")
	encodedTitle := strings.TrimPrefix(profileTitle, "base64:")
	decodedTitle, err := base64.StdEncoding.DecodeString(encodedTitle)
	if err != nil || string(decodedTitle) != "sub-store" {
		t.Fatalf("unexpected profile title header %q", profileTitle)
	}
	disposition, params, err := mime.ParseMediaType(recorder.Header().Get("Content-Disposition"))
	if err != nil || disposition != "inline" || params["filename"] != "sub-store" {
		t.Fatalf("unexpected content disposition %q", recorder.Header().Get("Content-Disposition"))
	}
}

func TestAccessKeyPublishesSelectedSubscriptionUsage(t *testing.T) {
	app := testApp(t)
	expiresAt := time.Date(2026, time.October, 1, 0, 0, 0, 0, time.UTC)
	const gib = uint64(1024 * 1024 * 1024)
	app.subscriptionClient = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		header := make(http.Header)
		header.Set("Subscription-Userinfo", fmt.Sprintf("upload=%d; download=%d; total=%d; expire=%d", gib, 2*gib, 10*gib, expiresAt.Unix()))
		body := "proxies:\n  - name: fresh\n    type: ss\n    server: fresh.example.com\n    port: 443\n    cipher: aes-128-gcm\n    password: secret\n"
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: header}, nil
	})}
	result, err := app.db.Exec(`INSERT INTO subscriptions(name,url,user_agent,enabled,created_at) VALUES('Selected usage','https://provider.example/selected','clash-meta',1,'now')`)
	if err != nil {
		t.Fatal(err)
	}
	subscriptionID, _ := result.LastInsertId()
	key := "selected-usage-key"
	createBody := fmt.Sprintf(`{"name":"Selected","key":%q,"monthlyDataGB":99,"usageSubscriptionId":%d}`, key, subscriptionID)
	createRecorder := httptest.NewRecorder()
	app.handleAccessKeys(createRecorder, httptest.NewRequest(http.MethodPost, "/api/access-keys", strings.NewReader(createBody)))
	if createRecorder.Code != http.StatusCreated {
		t.Fatalf("access key create returned %d: %s", createRecorder.Code, createRecorder.Body.String())
	}

	recorder := httptest.NewRecorder()
	app.handlePublicSubscription(recorder, httptest.NewRequest(http.MethodGet, "/sub/"+key, nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("subscription returned %d: %s", recorder.Code, recorder.Body.String())
	}
	usage := parseSubscriptionUserInfo(recorder.Header().Get("Subscription-Userinfo"))
	if math.Abs(usage.UsedGB-3) > 0.0001 || math.Abs(usage.TotalGB-10) > 0.0001 || usage.ExpireAt != expiresAt.Format(time.RFC3339) {
		t.Fatalf("selected subscription usage was not published: %+v", usage)
	}
	keys := app.listAccessKeys()
	if len(keys) != 1 || keys[0].UsageSubscriptionID != subscriptionID || keys[0].UsageSubscriptionName != "Selected usage" {
		t.Fatalf("usage subscription selection was not returned: %+v", keys)
	}
}

func TestPublicSubscriptionRefreshesEveryEnabledSubscriptionBeforeGeneration(t *testing.T) {
	app := testApp(t)
	app.db.SetMaxOpenConns(1)
	var firstRequests, secondRequests, disabledRequests atomic.Int32
	var activeRequests, maximumActiveRequests atomic.Int32
	var releaseRequests sync.Once
	bothRequestsStarted := make(chan struct{})
	app.subscriptionClient = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		active := activeRequests.Add(1)
		defer activeRequests.Add(-1)
		for {
			maximum := maximumActiveRequests.Load()
			if active <= maximum || maximumActiveRequests.CompareAndSwap(maximum, active) {
				break
			}
		}
		if active == 2 {
			releaseRequests.Do(func() { close(bothRequestsStarted) })
		}
		select {
		case <-bothRequestsStarted:
		case <-time.After(time.Second):
		}
		var name string
		switch request.URL.Path {
		case "/first":
			firstRequests.Add(1)
			name = "fresh-one"
		case "/second":
			secondRequests.Add(1)
			name = "fresh-two"
		case "/disabled":
			disabledRequests.Add(1)
			name = "disabled"
		default:
			return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(strings.NewReader("not found")), Header: make(http.Header)}, nil
		}
		body := fmt.Sprintf("proxies:\n  - name: %s\n    type: ss\n    server: %s.example.com\n    port: 443\n    cipher: aes-128-gcm\n    password: secret\n", name, name)
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})}

	oldSnapshot := "proxies:\n  - name: stale\n    type: ss\n    server: stale.example.com\n    port: 443\n    cipher: aes-128-gcm\n    password: secret\n"
	firstResult, err := app.db.Exec(`INSERT INTO subscriptions(name,url,user_agent,enabled,raw_content,created_at) VALUES('First',?,'clash-meta',1,?,'now')`, "https://provider.example/first", oldSnapshot)
	if err != nil {
		t.Fatal(err)
	}
	firstID, _ := firstResult.LastInsertId()
	if _, err := app.db.Exec(`INSERT INTO subscriptions(name,url,user_agent,enabled,raw_content,created_at) VALUES('Second',?,'clash-meta',1,?,'now'),('Disabled',?,'clash-meta',0,?,'now')`, "https://provider.example/second", oldSnapshot, "https://provider.example/disabled", oldSnapshot); err != nil {
		t.Fatal(err)
	}
	if _, err := app.db.Exec(`INSERT INTO proxy_groups(name,group_type,proxies_json,enabled,created_at) VALUES('Only First','select',?,1,'now')`, fmt.Sprintf(`["subscription-id:%d"]`, firstID)); err != nil {
		t.Fatal(err)
	}
	key := "refresh-all-test-key"
	if _, err := app.db.Exec(`INSERT INTO access_keys(name,key_hash,key_value,key_preview,enabled,created_at) VALUES('Refresh all',?,?,?,1,'now')`, hashToken(key), key, "re••ey"); err != nil {
		t.Fatal(err)
	}

	recorder := httptest.NewRecorder()
	app.handlePublicSubscription(recorder, httptest.NewRequest(http.MethodGet, "/sub/"+key, nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("subscription returned %d: %s", recorder.Code, recorder.Body.String())
	}
	if firstRequests.Load() != 1 || secondRequests.Load() != 1 {
		t.Fatalf("enabled subscriptions were not all refreshed: first=%d second=%d", firstRequests.Load(), secondRequests.Load())
	}
	if disabledRequests.Load() != 0 {
		t.Fatalf("disabled subscription was refreshed %d times", disabledRequests.Load())
	}
	if maximumActiveRequests.Load() < 2 {
		t.Fatalf("enabled subscriptions were refreshed sequentially; maximum concurrent requests=%d", maximumActiveRequests.Load())
	}
	config := recorder.Body.String()
	for _, name := range []string{"First / fresh-one", "Second / fresh-two"} {
		if !strings.Contains(config, name) {
			t.Fatalf("generated configuration does not contain refreshed proxy %q", name)
		}
	}
	if strings.Contains(config, " / stale") {
		t.Fatal("generated configuration used a stale subscription snapshot")
	}
}

func TestServiceRuleGroupsExpandInline(t *testing.T) {
	app := testApp(t)
	if _, err := app.db.Exec(`INSERT INTO proxy_groups(name,group_type,proxies_json,enabled,created_at) VALUES('Default','select','["DIRECT"]',1,'now'),('HomeIP','select','["DIRECT"]',1,'now')`); err != nil {
		t.Fatal(err)
	}
	if err := app.migrate(); err != nil {
		t.Fatal(err)
	}
	if _, err := app.db.Exec(`INSERT INTO subscriptions(name,url,user_agent,enabled,raw_content,created_at) VALUES('Fixture','https://provider.example/sub','clash-meta',1,?,'now')`, "proxies:\n  - name: SS fixture\n    type: ss\n    server: ss.example.com\n    port: 443\n    cipher: aes-128-gcm\n    password: secret\n"); err != nil {
		t.Fatal(err)
	}
	privateKey := "service-rules-test-key"
	if _, err := app.db.Exec(`INSERT INTO access_keys(name,key_hash,key_value,key_preview,enabled,created_at) VALUES('Test',?,?,?,1,'now')`, hashToken(privateKey), privateKey, "se••ey"); err != nil {
		t.Fatal(err)
	}
	groups := app.listServiceRuleGroups()
	if len(groups) != len(defaultServiceRules) {
		t.Fatalf("got %d service rule groups, want %d", len(groups), len(defaultServiceRules))
	}
	for _, group := range groups {
		if group.RuleCount != len(defaultServiceRules[group.Name]) || group.RuleCount == 0 {
			t.Fatalf("group %s has %d rules, want %d", group.Name, group.RuleCount, len(defaultServiceRules[group.Name]))
		}
	}
	request := httptest.NewRequest(http.MethodGet, "/sub/"+privateKey, nil)
	recorder := httptest.NewRecorder()
	app.handlePublicSubscription(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("private subscription returned %d", recorder.Code)
	}
	config := recorder.Body.String()
	var parsed map[string]any
	if err := yaml.Unmarshal([]byte(config), &parsed); err != nil {
		t.Fatalf("generated service config is invalid YAML: %v", err)
	}
	proxies := parsed["proxies"].([]any)
	if len(proxies) != 1 || proxies[0].(map[string]any)["cipher"] != "aes-128-gcm" {
		t.Fatalf("private subscription did not preserve Shadowsocks cipher")
	}
	for _, group := range groups {
		for _, rule := range defaultServiceRules[group.Name] {
			inline := "\n  - " + rule + "," + group.Target + "\n"
			if !strings.Contains(config, inline) {
				t.Fatalf("generated config is missing %s inline destination %q", group.Name, rule)
			}
		}
	}
	for _, expected := range []string{
		"DOMAIN-SUFFIX,openai.com,AI",
		"DOMAIN-SUFFIX,netflix.com,Netflix",
		"DOMAIN-SUFFIX,youtube.com,YouTube",
		"DOMAIN-SUFFIX,disneyplus.com,DisneyPlus",
		"DOMAIN-SUFFIX,steampowered.com,Game",
		"DOMAIN-SUFFIX,reddit.com,Reddit",
	} {
		if !strings.Contains(config, expected) {
			t.Errorf("generated config is missing inline rule %q", expected)
		}
	}
	for _, unsafe := range []string{"DOMAIN-SUFFIX,stripe.com,AI", "DOMAIN-SUFFIX,onetrust.com,Netflix", "DOMAIN-SUFFIX,execute-api.us-east-1.amazonaws.com,DisneyPlus"} {
		if strings.Contains(config, unsafe) {
			t.Errorf("generated config contains overly broad reference rule %q", unsafe)
		}
	}
}

func TestRuleTargetFollowsGroupRename(t *testing.T) {
	app := testApp(t)
	result, err := app.db.Exec(`INSERT INTO proxy_groups(name,group_type,proxies_json,enabled,created_at) VALUES('HOMEIP','select','["DIRECT"]',1,'now')`)
	if err != nil {
		t.Fatal(err)
	}
	groupID, _ := result.LastInsertId()
	if _, err = app.db.Exec(`INSERT INTO rules(rule_type,match_value,target,priority,enabled,created_at) VALUES('DOMAIN-SUFFIX','reddit.com','HOMEIP',55,1,'now')`); err != nil {
		t.Fatal(err)
	}

	// Running migrations on an existing installation backfills the stable ID
	// and adds the remaining editable Reddit rules once HOMEIP exists.
	if err = app.migrate(); err != nil {
		t.Fatal(err)
	}
	if _, err = app.db.Exec(`UPDATE proxy_groups SET name='RESIDENTIAL' WHERE id=?`, groupID); err != nil {
		t.Fatal(err)
	}

	rules := app.listRules()
	for _, rule := range rules {
		if strings.Contains(rule.Match, "reddit") || rule.Match == "redd.it" {
			if rule.TargetGroupID != groupID || rule.Target != "RESIDENTIAL" {
				t.Fatalf("rule %q points to target %q (id %d), want RESIDENTIAL (id %d)", rule.Match, rule.Target, rule.TargetGroupID, groupID)
			}
		}
	}
	config := app.generateConfig()
	if !strings.Contains(config, "DOMAIN-SUFFIX,reddit.com,RESIDENTIAL") {
		t.Fatalf("generated config did not resolve renamed group:\n%s", config)
	}
}

func TestManualProxyGroupMembershipFollowsRename(t *testing.T) {
	app := testApp(t)
	proxyResult, err := app.db.Exec(`INSERT INTO proxies(name,protocol,address,port,credential,enabled,created_at) VALUES('Old node','ss','proxy.example.com',443,'secret',1,'now')`)
	if err != nil {
		t.Fatal(err)
	}
	proxyID, _ := proxyResult.LastInsertId()
	if _, err := app.db.Exec(`INSERT INTO proxy_groups(name,group_type,proxies_json,enabled,created_at) VALUES('Manual route','select','["Old node"]',1,'now')`); err != nil {
		t.Fatal(err)
	}
	if err := app.migrateLegacyGroupReferences(); err != nil {
		t.Fatal(err)
	}
	groups := app.listGroups()
	if len(groups) != 1 || len(groups[0].Proxies) != 1 || groups[0].Proxies[0] != fmt.Sprintf("proxy-id:%d", proxyID) {
		t.Fatalf("manual proxy membership was not migrated to a stable ID: %+v", groups)
	}
	if _, err := app.db.Exec(`UPDATE proxies SET name='Renamed node' WHERE id=?`, proxyID); err != nil {
		t.Fatal(err)
	}
	if config := app.generateConfig(); !strings.Contains(config, "      - \"Renamed node\"") {
		t.Fatalf("generated config did not resolve renamed manual proxy:\n%s", config)
	}
}

func TestDefaultFakeIPFilterIsValidYAML(t *testing.T) {
	app := testApp(t)
	var config map[string]any
	if err := yaml.Unmarshal([]byte(app.generateConfig()), &config); err != nil {
		t.Fatalf("generated config is invalid YAML: %v", err)
	}
	dns, ok := config["dns"].(map[string]any)
	if !ok {
		t.Fatalf("generated config has no DNS map")
	}
	items, ok := dns["fake-ip-filter"].([]any)
	if !ok {
		t.Fatalf("generated config has no fake-ip-filter list")
	}
	got := map[string]bool{}
	for _, item := range items {
		got[item.(string)] = true
	}
	for _, want := range configList(defaultDNSFakeIPFilter) {
		if !got[want] {
			t.Errorf("fake-ip-filter is missing %q", want)
		}
	}
}

func TestPreferredRoutingGroupOrderMigration(t *testing.T) {
	app := testApp(t)
	if _, err := app.db.Exec(`INSERT INTO subscriptions(name,url,user_agent,enabled,created_at) VALUES('光喵','https://cheap.example/sub','clash-meta',1,'now')`); err != nil {
		t.Fatal(err)
	}
	if _, err := app.db.Exec(`INSERT INTO proxy_groups(name,group_type,proxies_json,enabled,created_at) VALUES
		('Default','select','["DIRECT","subscription-id:1","subscription-id:99"]',1,'now'),
		('HomeIP','select','["subscription-id:2"]',1,'now'),
		('Reddit','select','["DIRECT","group-id:2","group-id:1"]',1,'now')`); err != nil {
		t.Fatal(err)
	}
	if err := app.migratePreferredRoutingGroupOrder(); err != nil {
		t.Fatal(err)
	}
	groups := app.listGroups()
	byName := map[string][]string{}
	for _, group := range groups {
		byName[group.Name] = group.Proxies
	}
	if got := strings.Join(byName["Default"], ","); got != "subscription-id:1,subscription-id:99,DIRECT" {
		t.Fatalf("unexpected Default order: %s", got)
	}
	if got := strings.Join(byName["Reddit"], ","); got != "group-id:2,group-id:1,DIRECT" {
		t.Fatalf("unexpected Reddit order: %s", got)
	}
}

func TestPrivateSubscriptionTokenIsRedactedFromLogs(t *testing.T) {
	if got := safeLogPath("/sub/a-private-access-key"); got != "/sub/[redacted]" {
		t.Fatalf("private token leaked into log path: %s", got)
	}
	if got := safeLogPath("/api/subscriptions"); got != "/api/subscriptions" {
		t.Fatalf("ordinary API path changed: %s", got)
	}
}
