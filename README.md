# Wallet2 — микросервисы на Go

Репозиторий практического задания: кошелёк-обменник с авторизацией и сервис курсов
валют, которые общаются между собой по gRPC. Остальные сервисы добавляются по мере
готовности.

| Модуль | Роль | Транспорт | Хранилище |
|---|---|---|---|
| [`proto-exchange`](proto-exchange/README.md) | контракт gRPC: курсы валют и кросс-курс | — | — |
| [`gw-currency-wallet`](gw-currency-wallet/README.md) | регистрация/логин (JWT), баланс, пополнение, вывод, обмен | REST `:8080` | PostgreSQL `wallet` |

## Архитектура

```
клиент ──HTTP(JWT)──► gw-currency-wallet ──gRPC──► сервис курсов валют
                            │
                            └── Kafka: события о крупных операциях
```

Кошелёк — единственная точка входа для клиента. Курсы валют он получает по gRPC
и кэширует их на короткое время, поэтому обмен не делает лишний сетевой вызов.
Крупные операции публикуются в Kafka для последующей обработки истории и аналитики.

## Структура репозитория

```
Wallet2/
├── go.work                  # рабочее пространство Go-модулей
├── Makefile                 # proto / build / test / vet
├── deploy/                  # init-скрипты баз данных
├── docs/                    # документация
├── scripts/                 # dev-env.ps1, build.ps1, test.ps1, vet.ps1
├── proto-exchange/          # модуль github.com/proto-exchange/exchange_grpc
│   └── exchange/            # exchange.proto + сгенерированные стабы
└── gw-currency-wallet/      # REST API кошелька
```

Каждый сервис — отдельный Go-модуль со своими `go.mod`, `Makefile`, `config.env`,
`Dockerfile`, `README.md` и тестами.

## Устройство сервиса

Все сервисы собираются по одной схеме:

```
cmd/main.go                  флаг -c config.env -> config.LoadEnv -> logger -> app.New -> pkg/app.Run
internal/app/app.go          композиция: список компонентов, старт по порядку, стоп в обратном
internal/config/             Config + LoadEnv + значения по умолчанию
internal/logger/             slog: JSON, уровни DEBUG/INFO/WARN/ERROR, поле service
internal/storages/           интерфейс Storage + реализация под конкретную БД
internal/<транспорт>/        gRPC-сервер, HTTP-сервер, Kafka-продюсер
pkg/utils.go                 хелперы окружения (GetEnv, GetEnvInt, GetEnvDuration, GetEnvList)
pkg/app/app.go               Lifecycle-компоненты и graceful shutdown по SIGINT/SIGTERM
```

Компонент — всё, что имеет `Start(ctx)`/`Stop(ctx)`: пул БД, gRPC- или HTTP-сервер,
Kafka-продюсер и консьюмер. Приложение поднимает их по порядку и гасит в обратном,
поэтому соединения закрываются после остановки серверов.

## Требования

- Go 1.25+
- `protoc` + `protoc-gen-go` + `protoc-gen-go-grpc` (для перегенерации стабов)
- Docker Desktop (для PostgreSQL/Kafka)
- `make` (опционально; на Windows удобнее скрипты из `scripts/`)

## Быстрый старт (Windows, офлайн-окружение)

```powershell
# 1) сборка всех модулей (linux/amd64 бинари `main` + проверка proto-стабов)
powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\build.ps1

# 2) тесты и vet
powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\test.ps1
powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\vet.ps1
```

`scripts/build.ps1` кладёт рядом с каждым сервисом бинарник `main` (в `.gitignore`).

> `-ExecutionPolicy Bypass` обязателен: репозиторий на сетевом диске `Z:`, а политика
> `RemoteSigned` блокирует там неподписанные скрипты. Здесь Windows PowerShell 5.1
> (`powershell`), а не PowerShell 7 (`pwsh`).
>
> Скрипты сборки сами выставляют окружение (`scripts/dev-env.ps1`): локальные кэши Go,
> `GOOS=windows` для локальных команд, `GOWORK=off`, `file://`-прокси. Без этого в
> профиле остаётся `GOOS=linux`, а кэши в `AppData` недоступны на запись.

## Полезные команды

| Команда | Что делает |
|---|---|
| `make proto` | перегенерация `proto-exchange/exchange/*.pb.go` |
| `make build` | сборка всех модулей (`scripts/build.ps1`) |
| `make test` | `go test ./...` во всех модулях |
| `make vet` | `go vet ./...` во всех модулях |

## Статус

| Компонент | Состояние |
|---|---|
| `proto-exchange` | `exchange.proto`, генерация стабов, `go.mod` — готово |
| `gw-currency-wallet` | реализован: 7 REST-ручек с JWT, PostgreSQL + миграции, gRPC-клиент курсов с TTL-кэшем, события в Kafka, OpenAPI, тесты |
| инфраструктура | в сервисе есть `docker-compose.yml` (Postgres + Kafka + сервис); общий compose на весь стек — в работе |

Тесты: `go test ./...` в модуле `gw-currency-wallet` (сервисный и HTTP-уровень на фейках,
JWT, TTL-кэш курсов). Запуск и контракты — в [README сервиса](gw-currency-wallet/README.md).
