#!/usr/bin/env python3
"""
SubStore Password Recovery Tool

Generates a password hash using the exact same algorithm as SubStore
(v1$<hex-salt>$<hex-derived-key>, 16-byte salt, 120000 rounds of SHA-256)
and updates the admin password directly in the SQLite database.

Usage:
  1. Stop SubStore (to avoid SQLite WAL corruption).
  2. Run: python3 recover_password.py /path/to/data/substore.db --password "yournewpassword"
  3. Optionally pass --username to specify which user to update (default: first admin).
  4. Restart SubStore and log in with the new password.

Requires: Python 3.8+ (standard library only, no external dependencies)
"""

import argparse
import hashlib
import os
import secrets
import sqlite3
import sys


def derive_key(password: bytes, salt: bytes, rounds: int) -> bytes:
    """Replicates SubStore's deriveKey function exactly."""
    result = bytearray(salt)
    for _ in range(rounds):
        h = hashlib.sha256()
        h.update(result)
        h.update(password)
        result = bytearray(h.digest())
    return bytes(result)


def hash_password(password: str) -> str:
    """Replicates SubStore's hashPassword function exactly."""
    salt = secrets.token_bytes(16)
    derived = derive_key(password.encode("utf-8"), salt, 120000)
    return f"v1${salt.hex()}${derived.hex()}"


def main():
    parser = argparse.ArgumentParser(
        description="Recover a forgotten SubStore admin password by updating the database directly."
    )
    parser.add_argument(
        "db_path",
        help="Path to the substore.db file (e.g. data/substore.db)",
    )
    parser.add_argument(
        "--password",
        required=True,
        help="New password (minimum 8 characters)",
    )
    parser.add_argument(
        "--username",
        default="",
        help="Username to update (default: first admin user found)",
    )
    args = parser.parse_args()

    # Validate password length
    if len(args.password) < 8:
        print("Error: password must be at least 8 characters", file=sys.stderr)
        sys.exit(1)

    # Check database file exists
    if not os.path.exists(args.db_path):
        print(f"Error: database file not found at {args.db_path}", file=sys.stderr)
        sys.exit(1)

    # Open the database
    conn = sqlite3.connect(args.db_path)
    conn.row_factory = sqlite3.Row
    cursor = conn.cursor()

    # Find the admin user
    if args.username:
        cursor.execute(
            "SELECT id, username FROM users WHERE username=? AND role='admin' LIMIT 1",
            (args.username,),
        )
    else:
        cursor.execute(
            "SELECT id, username FROM users WHERE role='admin' ORDER BY id LIMIT 1"
        )

    row = cursor.fetchone()
    if row is None:
        print(
            f"Error: no admin user found{' with username ' + args.username if args.username else ''}",
            file=sys.stderr,
        )
        conn.close()
        sys.exit(1)

    user_id = row["id"]
    db_username = row["username"]
    print(f"Found admin user: {db_username} (id={user_id})")

    # Generate the new password hash
    new_hash = hash_password(args.password)

    # Update the password
    cursor.execute(
        "UPDATE users SET password_hash=? WHERE id=?",
        (new_hash, user_id),
    )

    if cursor.rowcount == 0:
        print("Error: no rows were updated — user not found", file=sys.stderr)
        conn.close()
        sys.exit(1)

    # Invalidate all existing sessions so the user must log in fresh
    cursor.execute("DELETE FROM sessions WHERE user_id=?", (user_id,))
    cleared = cursor.rowcount
    conn.commit()
    conn.close()

    print(f"Cleared {cleared} existing session(s) for this user.")
    print()
    print("Password updated successfully!")
    print(f"  User: {db_username}")
    print()
    print("Next steps:")
    print("  1. Restart SubStore")
    print("  2. Log in with your new password")
    print("  3. (Optional) Change the password again from Account Settings in the UI")


if __name__ == "__main__":
    main()
