package handler

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/Dronsan89/Wallet2/gw-currency-wallet/internal/service"
)

// authMiddleware проверяет JWT и кладёт данные пользователя в контекст.
func (h *Handler) authMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		const prefix = "Bearer "

		header := c.GetHeader("Authorization")
		if !strings.HasPrefix(header, prefix) {
			c.AbortWithStatusJSON(http.StatusUnauthorized, errorResponse{Error: "Unauthorized"})
			return
		}

		claims, err := h.auth.Parse(strings.TrimSpace(strings.TrimPrefix(header, prefix)))
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, errorResponse{Error: "Unauthorized"})
			return
		}

		userID, err := uuid.Parse(claims.UserID)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, errorResponse{Error: "Unauthorized"})
			return
		}

		c.Set(ctxUserID, userID.String())
		c.Set(ctxUsername, claims.Username)
		c.Next()
	}
}

// userIDFromContext достаёт идентификатор пользователя, установленный middleware.
func userIDFromContext(c *gin.Context) (uuid.UUID, bool) {
	raw, ok := c.Get(ctxUserID)
	if !ok {
		return uuid.Nil, false
	}

	value, ok := raw.(string)
	if !ok {
		return uuid.Nil, false
	}

	id, err := uuid.Parse(value)
	if err != nil {
		return uuid.Nil, false
	}

	return id, true
}

// register обрабатывает POST /api/v1/register.
func (h *Handler) register(c *gin.Context) {
	var req registerRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, errorResponse{Error: "Invalid request body"})
		return
	}

	err := h.service.Register(c.Request.Context(), req.Username, req.Password, req.Email)
	switch {
	case err == nil:
		c.JSON(http.StatusCreated, messageResponse{Message: "User registered successfully"})
	case errors.Is(err, service.ErrUserExists):
		c.JSON(http.StatusBadRequest, errorResponse{Error: "Username or email already exists"})
	case errors.Is(err, service.ErrInvalidInput):
		c.JSON(http.StatusBadRequest, errorResponse{Error: "Invalid request body"})
	default:
		h.log.Error("register failed", "request_id", c.GetString(ctxRequestID), "error", err)
		c.JSON(http.StatusInternalServerError, errorResponse{Error: "Internal server error"})
	}
}

// login обрабатывает POST /api/v1/login.
func (h *Handler) login(c *gin.Context) {
	var req loginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, errorResponse{Error: "Invalid request body"})
		return
	}

	token, err := h.service.Login(c.Request.Context(), req.Username, req.Password)
	switch {
	case err == nil:
		c.JSON(http.StatusOK, tokenResponse{Token: token})
	case errors.Is(err, service.ErrInvalidCredentials):
		c.JSON(http.StatusUnauthorized, errorResponse{Error: "Invalid username or password"})
	default:
		h.log.Error("login failed", "request_id", c.GetString(ctxRequestID), "error", err)
		c.JSON(http.StatusInternalServerError, errorResponse{Error: "Internal server error"})
	}
}