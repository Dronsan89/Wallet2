# proto-exchange

Модуль `github.com/proto-exchange/exchange_grpc`: протофайл и сгенерированные
gRPC-стабы для обмена между `gw-exchanger` и `gw-currency-wallet`.

```
proto-exchange/
├── exchange/
│   ├── exchange.proto
│   ├── exchange.pb.go
│   └── exchange_grpc.pb.go
├── go.mod
└── Makefile
```

## Генерация

```bash
make proto
# или вручную:
cd exchange
protoc --go_out=. --go_opt=paths=source_relative \
       --go-grpc_out=. --go-grpc_opt=paths=source_relative \
       --experimental_allow_proto3_optional exchange.proto
```

Импорт из сервисов:

```go
exchange "github.com/proto-exchange/exchange_grpc/exchange"
```

Подключение — через `replace` в `go.mod` сервиса:

```
require github.com/proto-exchange/exchange_grpc v0.0.0
replace github.com/proto-exchange/exchange_grpc => ../proto-exchange
```