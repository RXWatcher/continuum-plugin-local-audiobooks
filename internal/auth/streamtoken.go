// Package auth verifies presigned stream-URL JWTs minted by the audiobooks
// portal. The same shared HMAC secret signs and verifies; portal-side
// minting lives in silo-plugin-audiobooks.
package auth

import (
	"errors"
	"fmt"

	"github.com/golang-jwt/jwt/v5"
)

// expectedAudience is the canonical audience claim shared by every backend
// implementing the audiobook_backend.v1 byte routes. Matches the value
// bw-audio uses (internal/tokens) and the value the portal mints with
// (internal/mediatoken).
const expectedAudience = "audiobook_backend"

// StreamClaims is the verified subset of token claims callers need.
type StreamClaims struct {
	UserID  string
	BookID  string
	FileIdx int
}

// VerifyStreamToken parses + verifies token. Returns nil error iff:
//   - signature is valid HS256 against secret
//   - aud == "local_audiobooks"
//   - exp not exceeded
//   - book_id in token == expectedBookID
//   - file_idx in token == expectedFileIdx
func VerifyStreamToken(secret []byte, token string, expectedBookID string, expectedFileIdx int) (*StreamClaims, error) {
	if len(secret) == 0 {
		return nil, errors.New("empty signing secret")
	}
	if expectedBookID == "" {
		return nil, errors.New("expected book_id required")
	}
	if expectedFileIdx < 0 {
		return nil, errors.New("expected file_idx must be non-negative")
	}
	parsed, err := jwt.Parse(token, func(t *jwt.Token) (any, error) {
		// Restrict to HS256; reject "none" and asymmetric algs.
		if t.Method.Alg() != jwt.SigningMethodHS256.Alg() {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return secret, nil
	}, jwt.WithAudience(expectedAudience), jwt.WithExpirationRequired())
	if err != nil {
		return nil, fmt.Errorf("verify: %w", err)
	}
	if !parsed.Valid {
		return nil, errors.New("token invalid")
	}
	claims, ok := parsed.Claims.(jwt.MapClaims)
	if !ok {
		return nil, errors.New("claims not MapClaims")
	}
	bookID, _ := claims["book_id"].(string)
	if bookID != expectedBookID {
		return nil, fmt.Errorf("book_id mismatch (token=%q want=%q)", bookID, expectedBookID)
	}
	fidx, ok := claims["file_idx"].(float64)
	if !ok || fidx != float64(expectedFileIdx) {
		return nil, fmt.Errorf("file_idx mismatch")
	}
	sub, _ := claims["sub"].(string)
	if sub == "" {
		return nil, errors.New("sub required")
	}
	return &StreamClaims{UserID: sub, BookID: bookID, FileIdx: expectedFileIdx}, nil
}
