// Package server содержит HTTP-сервер сервиса кошелька.
package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"
)

// Server — HTTP-сервер с graceful shutdown.
type Server struct {
	srv  *http.Server
	addr string
	log  *slog.Logger
}

// New создаёт сервер на указанном адресе.
func New(addr string, handler http.Handler, log *slog.Logger) *Server {
	return &Server{
		addr: addr,
		log:  log,
		srv: &http.Server{
			Addr:              addr,
			Handler:           handler,
			ReadHeaderTimeout: 5 * time.Second,
			ReadTimeout:       15 * time.Second,
			WriteTimeout:      15 * time.Second,
			IdleTimeout:       60 * time.Second,
		},
	}
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

// Stop корректно останавливает сервер, дожидаясь активных запросов.
func (s *Server) Stop(ctx context.Context) error {
	if err := s.srv.Shutdown(ctx); err != nil {
		return fmt.Errorf("shutdown http server: %w", err)
	}

	return nil
}