package handler

import "github.com/shopspring/decimal"

// errorResponse — единый формат ошибки API.
type errorResponse struct {
	Error string `json:"error"`
}

// registerRequest — тело POST /api/v1/register.
type registerRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Email    string `json:"email"`
}

// loginRequest — тело POST /api/v1/login.
type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// amountRequest — тело пополнения и списания.
type amountRequest struct {
	Amount   decimal.Decimal `json:"amount"`
	Currency string          `json:"currency"`
}

// exchangeRequest — тело POST /api/v1/exchange.
type exchangeRequest struct {
	FromCurrency string          `json:"from_currency"`
	ToCurrency   string          `json:"to_currency"`
	Amount       decimal.Decimal `json:"amount"`
}

// messageResponse — сообщение об успехе без данных.
type messageResponse struct {
	Message string `json:"message"`
}

// tokenResponse — выданный JWT.
type tokenResponse struct {
	Token string `json:"token"`
}

// balanceResponse — баланс по валютам.
type balanceResponse struct {
	Balance map[string]float64 `json:"balance"`
}

// replenishResponse — ответ пополнения и списания.
type replenishResponse struct {
	Message    string             `json:"message"`
	NewBalance map[string]float64 `json:"new_balance"`
}

// ratesResponse — курсы валют.
type ratesResponse struct {
	Rates map[string]float64 `json:"rates"`
}

// exchangeResponse — результат обмена.
type exchangeResponse struct {
	Message         string             `json:"message"`
	ExchangedAmount float64            `json:"exchanged_amount"`
	NewBalance      map[string]float64 `json:"new_balance"`
}