// Package service содержит бизнес-логику кошелька.
package service

import "errors"

// Доменные ошибки сервиса. Транспортный слой переводит их в коды и тексты ответов.
var (
	// ErrInvalidInput — некорректная сумма, валюта или тело запроса.
	ErrInvalidInput = errors.New("invalid input")
	// ErrInvalidCredentials — неверная пара логин/пароль.
	ErrInvalidCredentials = errors.New("invalid credentials")
	// ErrUserExists — имя пользователя или email уже заняты.
	ErrUserExists = errors.New("user already exists")
	// ErrInsufficientFunds — на балансе недостаточно средств.
	ErrInsufficientFunds = errors.New("insufficient funds")
	// ErrRatesUnavailable — сервис курсов недоступен.
	ErrRatesUnavailable = errors.New("exchange rates unavailable")
)