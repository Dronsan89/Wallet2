package handler

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

// newTestRouter собирает минимальный роутер с тем же набором и порядком
// middleware, что и Router(), но с логгером, который пишет в буфер.
func newTestRouter(buf *bytes.Buffer) *gin.Engine {
	gin.SetMode(gin.TestMode)

	h := &Handler{log: slog.New(slog.NewJSONHandler(buf, nil))}

	router := gin.New()
	router.Use(requestIDMiddleware(), h.loggingMiddleware(), h.recoveryMiddleware())

	return router
}

// accessLog достаёт из буфера запись access-лога (msg = "http request").
func accessLog(t *testing.T, buf *bytes.Buffer) map[string]any {
	t.Helper()

	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if line == "" {
			continue
		}

		var entry map[string]any
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatalf("лог не является JSON: %q (%v)", line, err)
		}

		if entry["msg"] == "http request" {
			return entry
		}
	}

	t.Fatalf("access-лог не найден, буфер: %s", buf.String())
	return nil
}

func TestLoggingMiddlewareWritesAccessLog(t *testing.T) {
	var buf bytes.Buffer
	router := newTestRouter(&buf)
	router.GET("/ok", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"status": "ok"}) })

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/ok", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	entry := accessLog(t, &buf)
	if entry["path"] != "/ok" {
		t.Fatalf("path = %v, want /ok", entry["path"])
	}
	if entry["status"] != float64(http.StatusOK) {
		t.Fatalf("status в логе = %v, want 200", entry["status"])
	}
	if entry["request_id"] == "" || entry["request_id"] == nil {
		t.Fatalf("request_id не заполнен: %v", entry["request_id"])
	}
}

func TestPanicIsRecoveredAndLoggedAs500(t *testing.T) {
	var buf bytes.Buffer
	router := newTestRouter(&buf)
	router.GET("/panic", func(*gin.Context) { panic("boom") })

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/panic", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Internal server error") {
		t.Fatalf("body = %s, want JSON с ошибкой", rec.Body.String())
	}

	// Главное, что чинит правка: запрос с паникой тоже попадает в access-лог,
	// причём с настоящим статусом 500.
	entry := accessLog(t, &buf)
	if entry["status"] != float64(http.StatusInternalServerError) {
		t.Fatalf("status в логе = %v, want 500", entry["status"])
	}
	if !strings.Contains(buf.String(), "panic in http handler") {
		t.Fatalf("нет записи о панике: %s", buf.String())
	}
}
