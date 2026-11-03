// Package handler содержит HTTP-слой (Gin) сервиса кошелька.
package handler

import (
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/Dronsan89/Wallet2/gw-currency-wallet/docs"
	"github.com/Dronsan89/Wallet2/gw-currency-wallet/internal/auth"
	"github.com/Dronsan89/Wallet2/gw-currency-wallet/internal/service"
)

// Handler — HTTP-слой сервиса.
type Handler struct {
	service *service.Service
	auth    *auth.Manager
	log     *slog.Logger
}

// New создаёт HTTP-слой.
func New(svc *service.Service, authManager *auth.Manager, log *slog.Logger) *Handler {
	if log == nil {
		log = slog.Default()
	}

	return &Handler{service: svc, auth: authManager, log: log}
}

// Router собирает маршруты API.
func (h *Handler) Router() *gin.Engine {
	gin.SetMode(gin.ReleaseMode)

	router := gin.New()
	// Порядок важен: request-id нужен всем, логирование стоит снаружи recovery,
	// чтобы в access-лог попадал настоящий статус (500 после паники).
	router.Use(requestIDMiddleware(), h.loggingMiddleware(), h.recoveryMiddleware())

	router.GET("/health", h.health)
	router.GET("/swagger/doc.json", h.swaggerDoc)
	router.GET("/swagger/", h.swaggerUI)

	v1 := router.Group("/api/v1")
	v1.POST("/register", h.register)
	v1.POST("/login", h.login)

	authorized := v1.Group("", h.authMiddleware())
	authorized.GET("/balance", h.balance)
	authorized.POST("/wallet/deposit", h.deposit)
	authorized.POST("/wallet/withdraw", h.withdraw)
	authorized.GET("/exchange/rates", h.ratesHandler)
	authorized.POST("/exchange", h.exchangeHandler)

	return router
}

// health — проба для docker-compose.
func (h *Handler) health(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// swaggerDoc отдаёт OpenAPI-спецификацию.
func (h *Handler) swaggerDoc(c *gin.Context) {
	c.Data(http.StatusOK, "application/json; charset=utf-8", docs.SpecJSON())
}

// swaggerUI отдаёт страницу с ссылкой на спецификацию.
func (h *Handler) swaggerUI(c *gin.Context) {
	c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(swaggerPage))
}

const swaggerPage = `<!DOCTYPE html>
<html lang="ru">
<head>
    <meta charset="utf-8">
    <title>gw-currency-wallet API</title>
    <style>
        body { font-family: system-ui, sans-serif; margin: 2rem auto; max-width: 52rem; line-height: 1.5; }
        code { background: #f3f3f3; padding: 0.1rem 0.3rem; border-radius: 3px; }
        table { border-collapse: collapse; margin-top: 1rem; }
        th, td { border: 1px solid #ddd; padding: 0.35rem 0.6rem; text-align: left; }
    </style>
</head>
<body>
    <h1>gw-currency-wallet API</h1>
    <p>OpenAPI-спецификация: <a href="/swagger/doc.json">/swagger/doc.json</a></p>
    <table>
        <tr><th>Метод</th><th>Путь</th><th>Описание</th></tr>
        <tr><td>POST</td><td><code>/api/v1/register</code></td><td>регистрация пользователя</td></tr>
        <tr><td>POST</td><td><code>/api/v1/login</code></td><td>логин, возвращает JWT</td></tr>
        <tr><td>GET</td><td><code>/api/v1/balance</code></td><td>баланс по валютам</td></tr>
        <tr><td>POST</td><td><code>/api/v1/wallet/deposit</code></td><td>пополнение счёта</td></tr>
        <tr><td>POST</td><td><code>/api/v1/wallet/withdraw</code></td><td>вывод средств</td></tr>
        <tr><td>GET</td><td><code>/api/v1/exchange/rates</code></td><td>курсы валют</td></tr>
        <tr><td>POST</td><td><code>/api/v1/exchange</code></td><td>обмен валют</td></tr>
    </table>
    <p>Защищённые методы требуют заголовок <code>Authorization: Bearer &lt;JWT&gt;</code>.</p>
</body>
</html>
`