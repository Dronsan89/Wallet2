package auth

import (
	"errors"
	"testing"
	"time"
)

func TestTokenRoundTrip(t *testing.T) {
	manager := NewManager("secret", time.Hour)

	token, err := manager.NewToken("user-1", "alice")
	if err != nil {
		t.Fatalf("NewToken() error = %v", err)
	}

	claims, err := manager.Parse(token)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if claims.UserID != "user-1" || claims.Username != "alice" {
		t.Fatalf("claims = %+v", claims)
	}
}

func TestExpiredToken(t *testing.T) {
	manager := NewManager("secret", time.Hour)
	manager.now = func() time.Time { return time.Now().Add(-2 * time.Hour) }

	token, err := manager.NewToken("user-1", "alice")
	if err != nil {
		t.Fatalf("NewToken() error = %v", err)
	}

	manager.now = time.Now

	if _, err := manager.Parse(token); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("Parse() error = %v, want ErrInvalidToken", err)
	}
}

func TestTokenWithAlienSecret(t *testing.T) {
	first := NewManager("secret-1", time.Hour)
	token, err := first.NewToken("user-1", "alice")
	if err != nil {
		t.Fatalf("NewToken() error = %v", err)
	}

	second := NewManager("secret-2", time.Hour)
	if _, err := second.Parse(token); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("Parse() error = %v, want ErrInvalidToken", err)
	}
}