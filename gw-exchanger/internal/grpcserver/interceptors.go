package grpcserver

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// metadataRequestID — заголовок gRPC с идентификатором запроса.
const metadataRequestID = "x-request-id"

// requestIDKey — типизированный ключ контекста, чтобы не пересечься с чужими.
type requestIDKey struct{}

// requestIDFromContext достаёт идентификатор запроса из контекста.
func requestIDFromContext(ctx context.Context) string {
	if id, ok := ctx.Value(requestIDKey{}).(string); ok {
		return id
	}

	return ""
}

// requestIDInterceptor берёт x-request-id из метаданных или генерирует новый.
func requestIDInterceptor(ctx context.Context, req any, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
	id := ""

	if md, ok := metadata.FromIncomingContext(ctx); ok {
		if values := md.Get(metadataRequestID); len(values) > 0 {
			id = values[0]
		}
	}
	if id == "" {
		id = uuid.NewString()
	}

	return handler(context.WithValue(ctx, requestIDKey{}, id), req)
}

// loggingInterceptor пишет JSON-лог по каждому вызову.
func loggingInterceptor(log *slog.Logger) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		start := time.Now()

		resp, err := handler(ctx, req)

		attrs := []any{
			"method", info.FullMethod,
			"request_id", requestIDFromContext(ctx),
			"duration_ms", time.Since(start).Milliseconds(),
			"code", status.Code(err).String(),
		}

		if err != nil {
			log.Warn("grpc request failed", attrs...)
		} else {
			log.Info("grpc request", attrs...)
		}

		return resp, err
	}
}

// recoveryInterceptor превращает панику в Internal вместо падения процесса.
func recoveryInterceptor(log *slog.Logger) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (resp any, err error) {
		defer func() {
			if rec := recover(); rec != nil {
				log.Error("panic in grpc handler",
					"method", info.FullMethod,
					"request_id", requestIDFromContext(ctx),
					"panic", fmt.Sprint(rec),
				)
				err = status.Error(codes.Internal, "internal error")
			}
		}()

		return handler(ctx, req)
	}
}