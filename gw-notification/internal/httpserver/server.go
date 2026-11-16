// Package httpserver содержит служебный HTTP-сервер сервиса:
// пробы живости/готовности и счётчики обработки.
package httpserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/Dronsan89/Wallet2/gw-notification/internal/consumer"
	"github.com/Dronsan89/Wallet2/gw-notification/internal/storages"
)

// StatsProvider отдаёт счётчики обработки для ручки /stats.
type StatsProvider interface {
	Stats() consumer.Snapshot
}

// Server — служебный HTTP-сервер с graceful shutdown.
type Server struct {
	srv     *http.Server
	addr    string
	log     *slog.Logger
	storage storages.Storage
	stats   StatsProvider
}

// New создаёт сервер на указанном адресе.
func New(addr string, storage storages.Storage, stats StatsProvider, log *slog.Logger) *Server {
	s := &Server{
		addr:    addr,
		log:     log,
		storage: storage,
		stats:   stats,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/health", s.health)
	mux.HandleFunc("/ready", s.ready)
	mux.HandleFunc("/stats", s.statsHandler)

	s.srv = &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	return s
}

// Start открывает порт и обслуживает запросы в отдельной горутине.
func (s *Server) Start(_ context.Context) error {
	ln, err := net.Listen("tcp", s.addr)
	if err != nil {
		return fmt.Errorf("listen %s: %w", s.addr, err)
	}

	s.log.Info("http server started", "addr", s.addr)

	go func() {
		if err := s.srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			s.log.Error("http server stopped with error", "error", err)
		}
	}()

	return nil
}

// Stop корректно останавливает сервер.
func (s *Server) Stop(ctx context.Context) error {
	if err := s.srv.Shutdown(ctx); err != nil {
		return fmt.Errorf("shutdown http server: %w", err)
	}

	return nil
}

// health — сервис жив (не проверяет зависимости).
func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// ready — сервис готов принимать сообщения: проверяется доступность MongoDB.
func (s *Server) ready(w http.ResponseWriter, r *http.Request) {
	if err := s.storage.Ping(r.Context()); err != nil {
		s.log.Warn("readiness check failed", "error", err)
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{
			"status": "not ready",
			"error":  err.Error(),
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

// statsHandler отдаёт счётчики обработки сообщений.
func (s *Server) statsHandler(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.stats.Stats())
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}