# Общие команды монорепозитория.
# На Windows make из MinGW выполняет рецепты в cmd.exe, поэтому сборка и тесты
# делегируются PowerShell-скриптам: они выставляют GOOS/кэши/офлайн-прокси.
# -ExecutionPolicy Bypass нужен потому, что репозиторий лежит на сетевом диске
# (Z:), а политика RemoteSigned блокирует там неподписанные скрипты.

POWERSHELL := powershell -NoProfile -ExecutionPolicy Bypass -File

PROTO_DIR := proto-exchange/exchange
PROTO_CMD  := protoc --go_out=. --go_opt=paths=source_relative --go-grpc_out=. --go-grpc_opt=paths=source_relative --experimental_allow_proto3_optional exchange.proto

.PHONY: all proto build test vet up down

all: proto build test

proto:
	cd $(PROTO_DIR) && $(PROTO_CMD)

build:
	$(POWERSHELL) scripts/build.ps1

test:
	$(POWERSHELL) scripts/test.ps1

vet:
	$(POWERSHELL) scripts/vet.ps1

up:
	docker compose up -d

down:
	docker compose down
