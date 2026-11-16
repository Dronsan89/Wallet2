# gw-notification — сборка из корня репозитория:
#   docker build -f gw-notification/Dockerfile -t gw-notification .
#
# Внутри образа зависимости качаются с proxy.golang.org, поэтому для сборки нужен
# доступ в интернет (в офлайн-окружении используйте `go mod vendor`).

FROM golang:1.25-alpine AS builder

WORKDIR /src/gw-notification

COPY gw-notification/go.mod gw-notification/go.sum ./
RUN go mod download

COPY gw-notification ./
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o /out/main ./cmd

FROM gcr.io/distroless/static-debian12:nonroot

WORKDIR /app

COPY --from=builder /out/main /app/main
COPY --from=builder /src/gw-notification/config.env /app/config.env

USER nonroot:nonroot
EXPOSE 8082

ENTRYPOINT ["/app/main", "-c", "/app/config.env"]