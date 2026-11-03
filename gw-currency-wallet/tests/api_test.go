package tests

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Dronsan89/Wallet2/gw-currency-wallet/internal/handler"
)

func newTestRouter(env *testEnv) http.Handler {
	return handler.New(env.service, env.auth, testLogger()).Router()
}

func doJSON(t *testing.T, h http.Handler, method, path, token string, body any) *httptest.ResponseRecorder {
	t.Helper()

	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatalf("encode body: %v", err)
		}
	}

	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	return rec
}

func decode[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()

	var out T
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode response %q: %v", rec.Body.String(), err)
	}

	return out
}

func TestAPIFlow(t *testing.T) {
	env := newTestEnv(t)
	router := newTestRouter(env)

	register := map[string]string{
		"username": "alice",
		"password": "secret123",
		"email":    "alice@example.com",
	}

	rec := doJSON(t, router, http.MethodPost, "/api/v1/register", "", register)
	if rec.Code != http.StatusCreated {
		t.Fatalf("register: status = %d, body = %s", rec.Code, rec.Body.String())
	}

	rec = doJSON(t, router, http.MethodPost, "/api/v1/register", "", register)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("duplicate register: status = %d, want 400", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Username or email already exists") {
		t.Fatalf("duplicate register: body = %s", rec.Body.String())
	}

	rec = doJSON(t, router, http.MethodPost, "/api/v1/login", "", map[string]string{
		"username": "alice",
		"password": "secret123",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("login: status = %d, body = %s", rec.Code, rec.Body.String())
	}
	token := decode[struct {
		Token string `json:"token"`
	}](t, rec).Token
	if token == "" {
		t.Fatal("login: пустой токен")
	}

	rec = doJSON(t, router, http.MethodGet, "/api/v1/balance", "", nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("balance без токена: status = %d, want 401", rec.Code)
	}

	rec = doJSON(t, router, http.MethodGet, "/api/v1/balance", token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("balance: status = %d, body = %s", rec.Code, rec.Body.String())
	}

	rec = doJSON(t, router, http.MethodPost, "/api/v1/wallet/deposit", token, map[string]any{
		"amount":   100,
		"currency": "USD",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("deposit: status = %d, body = %s", rec.Code, rec.Body.String())
	}
	deposit := decode[struct {
		Message    string             `json:"message"`
		NewBalance map[string]float64 `json:"new_balance"`
	}](t, rec)
	if deposit.Message != "Account topped up successfully" {
		t.Fatalf("deposit: message = %q", deposit.Message)
	}
	if deposit.NewBalance["USD"] != 100 {
		t.Fatalf("deposit: USD = %v, want 100", deposit.NewBalance["USD"])
	}

	rec = doJSON(t, router, http.MethodGet, "/api/v1/exchange/rates", token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("rates: status = %d, body = %s", rec.Code, rec.Body.String())
	}
	ratesBody := decode[struct {
		Rates map[string]float64 `json:"rates"`
	}](t, rec)
	if ratesBody.Rates["USD"] != 90 {
		t.Fatalf("rates: USD = %v, want 90", ratesBody.Rates["USD"])
	}

	rec = doJSON(t, router, http.MethodPost, "/api/v1/exchange", token, map[string]any{
		"from_currency": "USD",
		"to_currency":   "EUR",
		"amount":        10,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("exchange: status = %d, body = %s", rec.Code, rec.Body.String())
	}
	exchange := decode[struct {
		Message         string             `json:"message"`
		ExchangedAmount float64            `json:"exchanged_amount"`
		NewBalance      map[string]float64 `json:"new_balance"`
	}](t, rec)
	if exchange.Message != "Exchange successful" {
		t.Fatalf("exchange: message = %q", exchange.Message)
	}
	if exchange.ExchangedAmount != 9 {
		t.Fatalf("exchange: exchanged_amount = %v, want 9", exchange.ExchangedAmount)
	}
	if exchange.NewBalance["EUR"] != 9 {
		t.Fatalf("exchange: EUR = %v, want 9", exchange.NewBalance["EUR"])
	}

	rec = doJSON(t, router, http.MethodPost, "/api/v1/wallet/withdraw", token, map[string]any{
		"amount":   10,
		"currency": "USD",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("withdraw: status = %d, body = %s", rec.Code, rec.Body.String())
	}

	rec = doJSON(t, router, http.MethodPost, "/api/v1/wallet/withdraw", token, map[string]any{
		"amount":   10000,
		"currency": "USD",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("withdraw без средств: status = %d, want 400", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Insufficient funds or invalid amount") {
		t.Fatalf("withdraw без средств: body = %s", rec.Body.String())
	}

	rec = doJSON(t, router, http.MethodGet, "/health", "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("health: status = %d", rec.Code)
	}

	rec = doJSON(t, router, http.MethodGet, "/swagger/doc.json", "", nil)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "openapi") {
		t.Fatalf("swagger: status = %d, body = %.80s", rec.Code, rec.Body.String())
	}
}

func TestAPIValidation(t *testing.T) {
	env := newTestEnv(t)
	router := newTestRouter(env)

	rec := doJSON(t, router, http.MethodPost, "/api/v1/register", "", map[string]string{
		"username": "alice",
		"password": "secret123",
		"email":    "alice@example.com",
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("register: status = %d", rec.Code)
	}

	rec = doJSON(t, router, http.MethodPost, "/api/v1/login", "", map[string]string{
		"username": "alice",
		"password": "secret123",
	})
	token := decode[struct {
		Token string `json:"token"`
	}](t, rec).Token

	rec = doJSON(t, router, http.MethodPost, "/api/v1/wallet/deposit", token, map[string]any{
		"amount":   10,
		"currency": "GBP",
	})
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "Invalid amount or currency") {
		t.Fatalf("deposit в GBP: status = %d, body = %s", rec.Code, rec.Body.String())
	}

	rec = doJSON(t, router, http.MethodGet, "/api/v1/balance", "not-a-jwt", nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("balance с плохим токеном: status = %d, want 401", rec.Code)
	}
}