package auth

import (
	"fmt"

	"golang.org/x/crypto/bcrypt"
)

// bcryptCost — стоимость хеширования паролей.
const bcryptCost = 12

// HashPassword хеширует пароль алгоритмом bcrypt.
func HashPassword(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcryptCost)
	if err != nil {
		return "", fmt.Errorf("hash password: %w", err)
	}

	return string(hash), nil
}

// CheckPassword сравнивает пароль с хешем.
func CheckPassword(hash, password string) error {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
}