package grpcserver

import (
	"context"
	"fmt"
	"log/slog"
	"net"

	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/reflection"

	exchange "github.com/proto-exchange/exchange_grpc/exchange"

	"github.com/Dronsan89/Wallet2/gw-exchanger/internal/config"
	"github.com/Dronsan89/Wallet2/gw-exchanger/internal/storages"
)

// Server — gRPC-сервер курсов с graceful shutdown.
type Server struct {
	cfg     *config.Config
	log     *slog.Logger
	storage storages.Storage

	grpc   *grpc.Server
	health *health.Server
}

// New создаёт сервер. Порт и зависимости берутся при Start.
func New(cfg *config.Config, storage storages.Storage, log *slog.Logger) *Server {
	if log == nil {
		log = slog.Default()
	}

	return &Server{cfg: cfg, log: log, storage: storage}
}

// NewGRPCServer собирает gRPC-сервер с сервисом курсов, health и reflection.
// Отдельная функция нужна, чтобы тесты могли поднять сервер на bufconn.
func NewGRPCServer(storage storages.Storage, log *slog.Logger) (*grpc.Server, *health.Server) {
	srv := grpc.NewServer(grpc.ChainUnaryInterceptor(
		requestIDInterceptor,
		loggingInterceptor(log),
		recoveryInterceptor(log),
	))

	exchange.RegisterExchangeServiceServer(srv, NewService(storage, log))

	healthSrv := health.NewServer()
	healthpb.RegisterHealthServer(srv, healthSrv)
	reflection.Register(srv)

	return srv, healthSrv
}

// Start открывает порт и обслуживает запросы в отдельной горутине.
func (s *Server) Start(_ context.Context) error {
	lis, err := net.Listen("tcp", ":"+s.cfg.GRPCPort)
	if err != nil {
		return fmt.Errorf("listen grpc port %s: %w", s.cfg.GRPCPort, err)
	}

	s.grpc, s.health = NewGRPCServer(s.storage, s.log)
	s.health.SetServingStatus("", healthpb.HealthCheckResponse_SERVING)

	go func() {
		if err := s.grpc.Serve(lis); err != nil {
			s.log.Error("grpc server stopped with error", "error", err)
		}
	}()

	s.log.Info("grpc server started", "addr", ":"+s.cfg.GRPCPort)

	return nil
}

// Stop сначала снимает health-статус, затем корректно останавливает сервер.
func (s *Server) Stop(ctx context.Context) error {
	if s.grpc == nil {
		return nil
	}

	if s.health != nil {
		s.health.SetServingStatus("", healthpb.HealthCheckResponse_NOT_SERVING)
	}

	done := make(chan struct{})
	go func() {
		s.grpc.GracefulStop()
		close(done)
	}()

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		s.grpc.Stop()
		s.log.Warn("grpc server stopped forcefully", "error", ctx.Err())
		return nil
	}
}