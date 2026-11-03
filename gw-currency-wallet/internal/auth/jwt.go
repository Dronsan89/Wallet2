// Package auth отвечает за JWT-аутентификацию и хеширование паролей.
package auth

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// ErrInvalidToken — токен отсутствует, просрочен или подписан неверным ключом.
var ErrInvalidToken = errors.New("invalid token")

// Claims — полезная нагрузка токена.
type Claims struct {
	UserID   string `json:"user_id"`
	Username string `json:"username"`
	jwt.RegisteredClaims
}

// Manager создаёт и проверяет JWT (HS256).
type Manager struct {
	secret []byte
	ttl    time.Duration
	now    func() time.Time
}

// NewManager создаёт менеджер токенов.
func NewManager(secret string, ttl time.Duration) *Manager {
	if ttl <= 0 {
		ttl = 24 * time.Hour
	}

	return &Manager{
		secret: []byte(secret),
		ttl:    ttl,
		now:    time.Now,
	}
}

// NewToken выпускает подписанный токен для пользователя.
func (m *Manager) NewToken(userID, username string) (string, error) {
	now := m.now()
	claims := Claims{
		UserID:   userID,
		Username: username,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   userID,
			Issuer:    "gw-currency-wallet",
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(m.ttl)),
		},
	}

	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(m.secret)
	if err != nil {
		return "", fmt.Errorf("sign token: %w", err)
	}

	return signed, nil
}

// Parse проверяет подпись и срок действия токена.
func (m *Manager) Parse(raw string) (*Claims, error) {
	claims := &Claims{}

	token, err := jwt.ParseWithClaims(raw, claims, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return m.secret, nil
	},
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithTimeFunc(m.now),
	)
	if err != nil || !token.Valid {
		return nil, ErrInvalidToken
	}

	return claims, nil
}