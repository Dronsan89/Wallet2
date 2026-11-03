package handler

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/Dronsan89/Wallet2/gw-currency-wallet/internal/rates"
	"github.com/Dronsan89/Wallet2/gw-currency-wallet/internal/service"
)

// ratesHandler обрабатывает GET /api/v1/exchange/rates.
func (h *Handler) ratesHandler(c *gin.Context) {
	all, err := h.service.Rates(c.Request.Context())
	if err != nil {
		h.log.Error("get rates failed", "request_id", c.GetString(ctxRequestID), "error", err)
		c.JSON(http.StatusInternalServerError, errorResponse{Error: "Failed to retrieve exchange rates"})
		return
	}

	c.JSON(http.StatusOK, ratesResponse{Rates: ratesToMap(all)})
}

// exchangeHandler обрабатывает POST /api/v1/exchange.
func (h *Handler) exchangeHandler(c *gin.Context) {
	userID, ok := userIDFromContext(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, errorResponse{Error: "Unauthorized"})
		return
	}

	var req exchangeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, errorResponse{Error: "Insufficient funds or invalid currencies"})
		return
	}

	exchanged, balances, err := h.service.Exchange(
		c.Request.Context(), userID, req.FromCurrency, req.ToCurrency, req.Amount,
	)
	switch {
	case err == nil:
		c.JSON(http.StatusOK, exchangeResponse{
			Message:         "Exchange successful",
			ExchangedAmount: exchanged.InexactFloat64(),
			NewBalance:      balancesToMap(balances),
		})
	case errors.Is(err, service.ErrInvalidInput), errors.Is(err, service.ErrInsufficientFunds):
		c.JSON(http.StatusBadRequest, errorResponse{Error: "Insufficient funds or invalid currencies"})
	case errors.Is(err, service.ErrRatesUnavailable):
		h.log.Error("exchange failed: rates unavailable", "request_id", c.GetString(ctxRequestID), "error", err)
		c.JSON(http.StatusInternalServerError, errorResponse{Error: "Failed to retrieve exchange rates"})
	default:
		h.log.Error("exchange failed", "request_id", c.GetString(ctxRequestID), "error", err)
		c.JSON(http.StatusInternalServerError, errorResponse{Error: "Internal server error"})
	}
}

// ratesToMap переводит курсы в JSON-числа.
func ratesToMap(all rates.Rates) map[string]float64 {
	out := make(map[string]float64, len(all))
	for currency, value := range all {
		out[currency] = value.InexactFloat64()
	}

	return out
}