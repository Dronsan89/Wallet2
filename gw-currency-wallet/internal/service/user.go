package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/Dronsan89/Wallet2/gw-currency-wallet/internal/auth"
	"github.com/Dronsan89/Wallet2/gw-currency-wallet/internal/storages"
)

// Register создаёт пользователя с нулевыми балансами по трём валютам.
func (s *Service) Register(ctx context.Context, username, password, email string) error {
	username = strings.TrimSpace(username)
	email = strings.ToLower(strings.TrimSpace(email))

	if err := validateRegistration(username, email, password); err != nil {
		return err
	}

	hash, err := auth.HashPassword(password)
	if err != nil {
		return fmt.Errorf("hash password: %w", err)
	}

	if _, err := s.storage.CreateUser(ctx, username, email, hash); err != nil {
		if errors.Is(err, storages.ErrUserExists) {
			return ErrUserExists
		}
		return fmt.Errorf("create user: %w", err)
	}

	s.log.Info("user registered", "username", username)

	return nil
}

// Login проверяет пароль и возвращает JWT.
func (s *Service) Login(ctx context.Context, username, password string) (string, error) {
	user, err := s.storage.UserByUsername(ctx, strings.TrimSpace(username))
	if err != nil {
		if errors.Is(err, storages.ErrUserNotFound) {
			return "", ErrInvalidCredentials
		}
		return "", fmt.Errorf("get user: %w", err)
	}

	if err := auth.CheckPassword(user.PasswordHash, password); err != nil {
		return "", ErrInvalidCredentials
	}

	token, err := s.auth.NewToken(user.ID.String(), user.Username)
	if err != nil {
		return "", fmt.Errorf("create token: %w", err)
	}

	s.log.Info("user logged in", "user_id", user.ID.String())

	return token, nil
}