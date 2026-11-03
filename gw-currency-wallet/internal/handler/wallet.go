package handler

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/Dronsan89/Wallet2/gw-currency-wallet/internal/service"
	"github.com/Dronsan89/Wallet2/gw-currency-wallet/internal/storages"
)

// balance обрабатывает GET /api/v1/balance.
func (h *Handler) balance(c *gin.Context) {
	userID, ok := userIDFromContext(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, errorResponse{Error: "Unauthorized"})
		return
	}

	balances, err := h.service.Balance(c.Request.Context(), userID)
	if err != nil {
		h.log.Error("get balance failed", "request_id", c.GetString(ctxRequestID), "error", err)
		c.JSON(http.StatusInternalServerError, errorResponse{Error: "Internal server error"})
		return
	}

	c.JSON(http.StatusOK, balanceResponse{Balance: balancesToMap(balances)})
}

// deposit обрабатывает POST /api/v1/wallet/deposit.
func (h *Handler) deposit(c *gin.Context) {
	userID, ok := userIDFromContext(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, errorResponse{Error: "Unauthorized"})
		return
	}

	var req amountRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, errorResponse{Error: "Invalid amount or currency"})
		return
	}

	balances, err := h.service.Deposit(c.Request.Context(), userID, req.Currency, req.Amount)
	switch {
	case err == nil:
		c.JSON(http.StatusOK, replenishResponse{
			Message:    "Account topped up successfully",
			NewBalance: balancesToMap(balances),
		})
	case errors.Is(err, service.ErrInvalidInput):
		c.JSON(http.StatusBadRequest, errorResponse{Error: "Invalid amount or currency"})
	default:
		h.log.Error("deposit failed", "request_id", c.GetString(ctxRequestID), "error", err)
		c.JSON(http.StatusInternalServerError, errorResponse{Error: "Internal server error"})
	}
}

// withdraw обрабатывает POST /api/v1/wallet/withdraw.
func (h *Handler) withdraw(c *gin.Context) {
	userID, ok := userIDFromContext(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, errorResponse{Error: "Unauthorized"})
		return
	}

	var req amountRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, errorResponse{Error: "Insufficient funds or invalid amount"})
		return
	}

	balances, err := h.service.Withdraw(c.Request.Context(), userID, req.Currency, req.Amount)
	switch {
	case err == nil:
		c.JSON(http.StatusOK, replenishResponse{
			Message:    "Withdrawal successful",
			NewBalance: balancesToMap(balances),
		})
	case errors.Is(err, service.ErrInvalidInput), errors.Is(err, service.ErrInsufficientFunds):
		c.JSON(http.StatusBadRequest, errorResponse{Error: "Insufficient funds or invalid amount"})
	default:
		h.log.Error("withdraw failed", "request_id", c.GetString(ctxRequestID), "error", err)
		c.JSON(http.StatusInternalServerError, errorResponse{Error: "Internal server error"})
	}
}

// balancesToMap переводит баланс в JSON-числа.
func balancesToMap(balances storages.Balances) map[string]float64 {
	out := make(map[string]float64, len(balances))
	for currency, amount := range balances {
		out[currency] = amount.InexactFloat64()
	}

	return out
}