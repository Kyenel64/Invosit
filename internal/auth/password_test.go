package auth

import (
	"errors"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

func TestHashAndVerifyRoundtrip(t *testing.T) {
	const pw = "correct horse battery staple"

	hash, err := HashPassword(pw)
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if hash == "" {
		t.Fatal("HashPassword returned empty hash")
	}
	if hash == pw {
		t.Fatal("hash equals plaintext (catastrophic)")
	}

	if err := VerifyPassword(hash, pw); err != nil {
		t.Errorf("VerifyPassword on correct password: %v", err)
	}
}

func TestVerifyPasswordRejectsWrong(t *testing.T) {
	hash, err := HashPassword("right password")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}

	err = VerifyPassword(hash, "wrong password")
	if !errors.Is(err, bcrypt.ErrMismatchedHashAndPassword) {
		t.Errorf("expected ErrMismatchedHashAndPassword, got %v", err)
	}
}

func TestHashPasswordTooLong(t *testing.T) {
	// bcrypt limit is 72 bytes; 73 ASCII chars is 73 bytes.
	pw := strings.Repeat("a", 73)

	_, err := HashPassword(pw)
	if !errors.Is(err, ErrPasswordTooLong) {
		t.Errorf("expected ErrPasswordTooLong, got %v", err)
	}
}
