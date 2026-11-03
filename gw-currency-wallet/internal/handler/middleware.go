package handler

import (
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// Ключи контекста запроса и служебный заголовок.
const (
	ctxUserID    = "user_id"
	ctxUsername  = "username"
	ctxRequestID = "request_id"

	headerRequestID = "X-Request-ID"
)

// requestIDMiddleware добавляет к запросу идентификатор и возвращает его в ответе.
func requestIDMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.GetHeader(headerRequestID)
		if id == "" {
			id = uuid.NewString()
		}

		c.Set(ctxRequestID, id)
		c.Header(headerRequestID, id)
		c.Next()
	}
}

// loggingMiddleware пишет JSON-лог по каждому запросу.
//
// Лог пишется в defer, а сам middleware стоит снаружи recoveryMiddleware:
// тогда паника в обработчике сначала превращается в 500 (recovery), и только
// после этого пишется access-лог — уже с настоящим статусом ответа.
func (h *Handler) loggingMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()

		defer func() {
			h.log.Info("http request",
				"request_id", c.GetString(ctxRequestID),
				"method", c.Request.Method,
				"path", c.FullPath(),
				"status", c.Writer.Status(),
				"duration_ms", time.Since(start).Milliseconds(),
				"user_id", c.GetString(ctxUserID),
			)
		}()

		c.Next()
	}
}

// recoveryMiddleware не даёт панике в обработчике уронить процесс.
func (h *Handler) recoveryMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if rec := recover(); rec != nil {
				h.log.Error("panic in http handler",
					"request_id", c.GetString(ctxRequestID),
					"path", c.FullPath(),
					"panic", fmt.Sprint(rec),
				)
				c.AbortWithStatusJSON(http.StatusInternalServerError, errorResponse{Error: "Internal server error"})
			}
		}()

		c.Next()
	}
}