// Package protect encrypts small secrets at rest for the current user. On Windows it uses
// DPAPI; the Linux backend keeps a per-user key file. Every failure is an error, never a
// silent fallback to plaintext, so callers can fail closed.
package protect

import "errors"

// Protector seals and unseals data for the current user only.
type Protector interface {
	Protect(plain []byte) ([]byte, error)
	Unprotect(sealed []byte) ([]byte, error)
}

// ErrUnreadable means sealed data exists but cannot be decrypted: it was tampered with,
// written by another user, or its key is gone.
var ErrUnreadable = errors.New("protected data cannot be decrypted by this user")

// entropy binds sealed data to Ullage's purpose, so a blob produced by another program for
// the same user cannot be substituted.
var entropy = []byte("ullage/redaction-rules/v1")
