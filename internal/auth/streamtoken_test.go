package auth_test

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/RXWatcher/continuum-plugin-local-audiobooks/internal/auth"
)

func mint(t *testing.T, secret []byte, claims jwt.MapClaims) string {
	t.Helper()
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	s, err := tok.SignedString(secret)
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	return s
}

func TestVerifyStreamToken_HappyPath(t *testing.T) {
	secret := []byte("0123456789abcdef0123456789abcdef")
	tok := mint(t, secret, jwt.MapClaims{
		"sub":      "u-1",
		"aud":      "audiobook_backend",
		"book_id":  "abc123",
		"file_idx": float64(0),
		"exp":      time.Now().Add(5 * time.Minute).Unix(),
	})
	got, err := auth.VerifyStreamToken(secret, tok, "abc123", 0)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if got.UserID != "u-1" {
		t.Errorf("UserID = %q", got.UserID)
	}
}

func TestVerifyStreamToken_RejectsExpired(t *testing.T) {
	secret := []byte("0123456789abcdef0123456789abcdef")
	tok := mint(t, secret, jwt.MapClaims{
		"sub": "u-1", "aud": "audiobook_backend", "book_id": "abc", "file_idx": float64(0),
		"exp": time.Now().Add(-1 * time.Minute).Unix(),
	})
	if _, err := auth.VerifyStreamToken(secret, tok, "abc", 0); err == nil {
		t.Fatal("expected expired error")
	}
}

func TestVerifyStreamToken_RejectsWrongAudience(t *testing.T) {
	secret := []byte("0123456789abcdef0123456789abcdef")
	tok := mint(t, secret, jwt.MapClaims{
		"sub": "u-1", "aud": "wrong", "book_id": "abc", "file_idx": float64(0),
		"exp": time.Now().Add(5 * time.Minute).Unix(),
	})
	if _, err := auth.VerifyStreamToken(secret, tok, "abc", 0); err == nil {
		t.Fatal("expected audience error")
	}
}

func TestVerifyStreamToken_RejectsBookIDMismatch(t *testing.T) {
	secret := []byte("0123456789abcdef0123456789abcdef")
	tok := mint(t, secret, jwt.MapClaims{
		"sub": "u-1", "aud": "audiobook_backend", "book_id": "abc", "file_idx": float64(0),
		"exp": time.Now().Add(5 * time.Minute).Unix(),
	})
	if _, err := auth.VerifyStreamToken(secret, tok, "different", 0); err == nil {
		t.Fatal("expected book_id mismatch")
	}
}

func TestVerifyStreamToken_RejectsBadSignature(t *testing.T) {
	secret := []byte("0123456789abcdef0123456789abcdef")
	tok := mint(t, []byte("different-secret-32-bytes-aaaaaaa"), jwt.MapClaims{
		"sub": "u-1", "aud": "audiobook_backend", "book_id": "abc", "file_idx": float64(0),
		"exp": time.Now().Add(5 * time.Minute).Unix(),
	})
	if _, err := auth.VerifyStreamToken(secret, tok, "abc", 0); err == nil {
		t.Fatal("expected signature error")
	}
}

func TestVerifyStreamToken_RejectsMissingSubject(t *testing.T) {
	secret := []byte("0123456789abcdef0123456789abcdef")
	tok := mint(t, secret, jwt.MapClaims{
		"aud": "audiobook_backend", "book_id": "abc", "file_idx": float64(0),
		"exp": time.Now().Add(5 * time.Minute).Unix(),
	})
	if _, err := auth.VerifyStreamToken(secret, tok, "abc", 0); err == nil {
		t.Fatal("expected missing subject error")
	}
}

func TestVerifyStreamToken_RejectsFractionalFileIndex(t *testing.T) {
	secret := []byte("0123456789abcdef0123456789abcdef")
	tok := mint(t, secret, jwt.MapClaims{
		"sub": "u-1", "aud": "audiobook_backend", "book_id": "abc", "file_idx": float64(0.5),
		"exp": time.Now().Add(5 * time.Minute).Unix(),
	})
	if _, err := auth.VerifyStreamToken(secret, tok, "abc", 0); err == nil {
		t.Fatal("expected fractional file_idx error")
	}
}

func TestVerifyStreamToken_RejectsNegativeExpectedIndex(t *testing.T) {
	secret := []byte("0123456789abcdef0123456789abcdef")
	tok := mint(t, secret, jwt.MapClaims{
		"sub": "u-1", "aud": "audiobook_backend", "book_id": "abc", "file_idx": float64(-1),
		"exp": time.Now().Add(5 * time.Minute).Unix(),
	})
	if _, err := auth.VerifyStreamToken(secret, tok, "abc", -1); err == nil {
		t.Fatal("expected negative expected index error")
	}
}
