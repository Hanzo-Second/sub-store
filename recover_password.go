//go:build ignore
// +build ignore

// This file is excluded from normal builds.
// It is a standalone password recovery tool — use recover_password.py instead
// (Python version requires no compiler and uses only the standard library).

package main

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"flag"
	"fmt"
	"os"

	_ "modernc.org/sqlite"
)

func hashPassword(password string) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	derived := deriveKey([]byte(password), salt, 120000)
	return "v1$" + hex.EncodeToString(salt) + "$" + hex.EncodeToString(derived), nil
}

func deriveKey(password, salt []byte, rounds int) []byte {
	result := append([]byte{}, salt...)
	for i := 0; i < rounds; i++ {
		h := sha256.New()
		h.Write(result)
		h.Write(password)
		result = h.Sum(nil)
	}
	return result
}

func main() {
	dbPath := flag.String("db", "data/substore.db", "path to the substore.db file")
	username := flag.String("username", "", "username to update (default: first admin user)")
	password := flag.String("password", "", "new password (min 8 chars)")
	flag.Parse()

	if len(*password) < 8 {
		fmt.Fprintln(os.Stderr, "Error: password must be at least 8 characters")
		os.Exit(1)
	}

	if _, err := os.Stat(*dbPath); os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "Error: database file not found at %s\n", *dbPath)
		os.Exit(1)
	}

	dsn := *dbPath + "?_pragma=busy_timeout(5000)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error opening database: %v\n", err)
		os.Exit(1)
	}
	defer db.Close()

	var userID int
	var dbUsername string
	if *username != "" {
		err = db.QueryRow(`SELECT id, username FROM users WHERE username=? AND role='admin' LIMIT 1`, *username).Scan(&userID, &dbUsername)
	} else {
		err = db.QueryRow(`SELECT id, username FROM users WHERE role='admin' ORDER BY id LIMIT 1`).Scan(&userID, &dbUsername)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error finding admin user: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Found admin user: %s (id=%d)\n", dbUsername, userID)

	hash, err := hashPassword(*password)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error generating hash: %v\n", err)
		os.Exit(1)
	}

	result, err := db.Exec(`UPDATE users SET password_hash=? WHERE id=?`, hash, userID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error updating password: %v\n", err)
		os.Exit(1)
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		fmt.Fprintln(os.Stderr, "Error: no rows were updated — user not found")
		os.Exit(1)
	}

	_, err = db.Exec(`DELETE FROM sessions WHERE user_id=?`, userID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not clear sessions: %v\n", err)
	} else {
		fmt.Println("Cleared all existing sessions for this user.")
	}

	fmt.Println()
	fmt.Println("Password updated successfully!")
	fmt.Printf("  User: %s\n", dbUsername)
	fmt.Println()
	fmt.Println("Next steps:")
	fmt.Println("  1. Restart SubStore")
	fmt.Println("  2. Log in with your new password")
	fmt.Println("  3. (Optional) Change the password again from Account Settings in the UI")
}
