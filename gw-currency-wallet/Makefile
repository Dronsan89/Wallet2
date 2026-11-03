# $(APP) — сборка/запуск/тесты.
# Команды рассчитаны на Linux/macOS/CI. На Windows используйте scripts/*.ps1
# в корне репозитория (они выставляют GOOS и локальные кэши Go).

APP := gw-currency-wallet

.PHONY: build run test vet proto

build:
	GOOS=linux GOARCH=amd64 go build -trimpath -o main ./cmd

run:
	go run ./cmd -c config.env

test:
	go test ./...

vet:
	go vet ./...

proto:
	cd ../proto-exchange/exchange && protoc --go_out=. --go_opt=paths=source_relative --go-grpc_out=. --go-grpc_opt=paths=source_relative --experimental_allow_proto3_optional exchange.proto