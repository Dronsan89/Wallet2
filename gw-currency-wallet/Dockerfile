# gw-currency-wallet — сборка из корня репозитория:
#   docker build -f gw-currency-wallet/Dockerfile -t gw-currency-wallet .
#
# Внутри образа зависимости качаются с proxy.golang.org, поэтому для сборки нужен
# доступ в интернет. В офлайн-окружении соберите образ из vendor-каталога
# (go mod vendor) или используйте готовый бинарь.

FROM golang:1.25-alpine AS builder

WORKDIR /src

COPY proto-exchange/go.mod proto-exchange/go.sum ./proto-exchange/
COPY proto-exchange/exchange ./proto-exchange/exchange
COPY gw-currency-wallet/go.mod gw-currency-wallet/go.sum ./gw-currency-wallet/

WORKDIR /src/gw-currency-wallet
RUN go mod download

COPY gw-currency-wallet ./
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o /out/main ./cmd

FROM gcr.io/distroless/static-debian12:nonroot

WORKDIR /app

COPY --from=builder /out/main /app/main
COPY --from=builder /src/gw-currency-wallet/migrations /app/migrations
COPY --from=builder /src/gw-currency-wallet/config.env /app/config.env

USER nonroot:nonroot
EXPOSE 8080

ENTRYPOINT ["/app/main", "-c", "/app/config.env"]