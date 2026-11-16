# gw-exchanger — сборка из корня репозитория:
#   docker build -f gw-exchanger/Dockerfile -t gw-exchanger .
#
# Внутри образа зависимости качаются с proxy.golang.org, поэтому для сборки нужен
# доступ в интернет (в офлайн-окружении используйте `go mod vendor`).

FROM golang:1.25-alpine AS builder

WORKDIR /src

COPY proto-exchange/go.mod proto-exchange/go.sum ./proto-exchange/
COPY proto-exchange/exchange ./proto-exchange/exchange
COPY gw-exchanger/go.mod gw-exchanger/go.sum ./gw-exchanger/

WORKDIR /src/gw-exchanger
RUN go mod download

COPY gw-exchanger ./
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o /out/main ./cmd \
 && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o /out/grpcprobe ./cmd/grpcprobe

FROM gcr.io/distroless/static-debian12:nonroot

WORKDIR /app

COPY --from=builder /out/main /app/main
COPY --from=builder /out/grpcprobe /app/grpcprobe
COPY --from=builder /src/gw-exchanger/migrations /app/migrations
COPY --from=builder /src/gw-exchanger/config.env /app/config.env

USER nonroot:nonroot
EXPOSE 50051

ENTRYPOINT ["/app/main", "-c", "/app/config.env"]