# Wallet2 — микросервисный кошелёк-обменник на Go

Четыре сервиса и общий gRPC-контракт: REST API кошелька с JWT-авторизацией,
сервис курсов валют, история крупных переводов и аналитика по потоку операций.

| Компонент | Роль | Интерфейс | Хранилище |
|---|---|---|---|
| [`proto-exchange`](proto-exchange/README.md) | gRPC-контракт: курсы валют и кросс-курс | — | — |
| [`gw-exchanger`](gw-exchanger/README.md) | курсы USD/RUB/EUR, кросс-курс | gRPC `:50051` | PostgreSQL `exchanger` |
| [`gw-currency-wallet`](gw-currency-wallet/README.md) | регистрация/логин (JWT), баланс, пополнение, вывод, обмен | REST `:8080` | PostgreSQL `wallet` |
| [`gw-notification`](gw-notification/README.md) | история крупных переводов | Kafka → `:8082` | MongoDB `notification` |
| [`gw-analytics`](gw-analytics/README.md) | метрики и агрегации 1m/5m/1h/1d/1w | Kafka → HTTP `:8083` | ClickHouse `analytics` |

## Возможности

- **Аутентификация**: регистрация и логин, JWT HS256, bcrypt (cost 12).
- **Мультивалютный кошелёк**: балансы USD/RUB/EUR, пополнение и вывод средств.
- **Обмен валют** по кросс-курсу сервиса курсов; курсы кэшируются на короткий TTL.
- **События о крупных операциях** (рублёвый эквивалент ≥ 30 000) публикуются в Kafka.
- **История переводов** в MongoDB с идемпотентностью по `transaction_id` и DLQ для
  необрабатываемых сообщений.
- **Аналитика**: количество событий по типам/статусам, задержка доставки, частота
  ошибок, агрегации за 1m/5m/1h/1d/1w.
- **Наблюдаемость**: структурные JSON-логи, пробы `/health` и `/ready`, счётчики
  обработки, OpenAPI-спецификация кошелька.

## Архитектура

```
клиент ──HTTP(JWT)──► gw-currency-wallet ──gRPC──► gw-exchanger ──► PostgreSQL (курсы)
                            │
                            └── Kafka: wallet.transactions
                                     ├──► gw-notification ──► MongoDB (история переводов)
                                     └──► gw-analytics    ──► ClickHouse (метрики)
```

Кошелёк — единственная точка входа для клиента. Курсы он получает по gRPC у
`gw-exchanger` и кэширует их (`RATES_CACHE_TTL`), поэтому обмен не делает лишний
сетевой вызов. Операции с рублёвым эквивалентом от `BIG_TRANSFER_THRESHOLD`
публикуются в топик `wallet.transactions`; `gw-notification` и `gw-analytics`
читают его **независимыми consumer group**, поэтому не конкурируют за сообщения.

## Быстрый старт

Требуется Docker с Compose:

```bash
docker compose up --build
```

Поднимаются PostgreSQL (две базы), Kafka (KRaft), MongoDB, ClickHouse и четыре сервиса.

| Что | Адрес |
|---|---|
| REST API кошелька | http://localhost:8080 |
| Служебные ручки notification | http://localhost:8082/health, `/ready`, `/stats` |
| Аналитика | http://localhost:8083/api/v1/analytics/summary?period=5m |
| gRPC курсов | `localhost:50051` |
| PostgreSQL / Kafka / MongoDB / ClickHouse | `5432` / `9092` / `27017` / `8123`, `9000` |

Проверка сценария:

```bash
curl -X POST localhost:8080/api/v1/register -H 'Content-Type: application/json' \
  -d '{"username":"alice","password":"secret123","email":"alice@example.com"}'

TOKEN=$(curl -s -X POST localhost:8080/api/v1/login -H 'Content-Type: application/json' \
  -d '{"username":"alice","password":"secret123"}' | sed -E 's/.*"token":"([^"]+)".*/\1/')

curl -X POST localhost:8080/api/v1/wallet/deposit -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d '{"amount":100,"currency":"USD"}'

curl -s localhost:8080/api/v1/exchange/rates -H "Authorization: Bearer $TOKEN"

curl -X POST localhost:8080/api/v1/exchange -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"from_currency":"USD","to_currency":"EUR","amount":10}'

curl -s "localhost:8083/api/v1/analytics/summary?period=5m"
```

Остановка: `docker compose down` (данные сохраняются в томах) или `down -v`
(удалить тома).

## Локальная сборка и тесты

Требования: Go 1.25+, `protoc` с плагинами `protoc-gen-go` и `protoc-gen-go-grpc`
(нужны только для перегенерации стабов), `make` (опционально).

```bash
make build     # linux/amd64 бинари всех сервисов + проверка proto-стабов
make test      # go test ./... во всех модулях
make vet       # go vet ./... во всех модулях
make proto     # перегенерация proto-exchange/exchange/*.pb.go
```

На Windows те же действия выполняют скрипты:

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\build.ps1
powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\test.ps1
powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\vet.ps1
```

Скрипты выставляют окружение сборки (локальные кэши Go, `GOOS`, `GOWORK=off`,
локальный модульный прокси) — это делает сборку воспроизводимой на машине без
доступа к внешним прокси. Каждый сервис запускается как обычно:

```bash
cd gw-currency-wallet && ./main -c config.env
```

## API

Полные контракты и примеры — в README сервисов; ниже сводка.

**gw-currency-wallet** (REST, JSON, `Authorization: Bearer <JWT>` для защищённых ручек):

| Метод | Путь | Назначение |
|---|---|---|
| POST | `/api/v1/register` | регистрация (201 / 400) |
| POST | `/api/v1/login` | логин, выдаёт JWT (200 / 401) |
| GET | `/api/v1/balance` | баланс по трём валютам |
| POST | `/api/v1/wallet/deposit` | пополнение |
| POST | `/api/v1/wallet/withdraw` | вывод средств |
| GET | `/api/v1/exchange/rates` | курсы валют |
| POST | `/api/v1/exchange` | обмен валют |

Дополнительно: `GET /health`, OpenAPI по `/swagger/doc.json`, страница `/swagger/`.

**gw-exchanger** (gRPC): `ExchangeService/GetExchangeRates`,
`ExchangeService/GetExchangeRateForCurrency`, плюс `grpc.health.v1` и reflection.

**gw-notification**: `/health`, `/ready` (проверка MongoDB), `/stats` (счётчики обработки).

**gw-analytics**: `GET /api/v1/analytics/summary?period=…`,
`GET /api/v1/analytics/timeseries?period=…&step=…`, `/health`, `/ready`, `/stats`.

## Конфигурация

Каждый сервис читает `config.env` (флаг `-c`) и переменные окружения; переменные
окружения приоритетнее файла. Ключевые параметры:

| Параметр | Где | По умолчанию |
|---|---|---|
| `HTTP_PORT` | wallet / notification / analytics | `8080` / `8082` / `8083` |
| `GRPC_PORT` | exchanger | `50051` |
| `DB_*` / `DATABASE_URL` | wallet, exchanger | `wallet` / `exchanger` |
| `JWT_SECRET` (из окружения), `JWT_TTL` | wallet | — / `24h` |
| `RATES_CACHE_TTL`, `EXCHANGER_GRPC_ADDR` | wallet | `5s`, `localhost:50051` |
| `BIG_TRANSFER_THRESHOLD` | wallet | `30000` (рубли) |
| `KAFKA_BROKERS`, `KAFKA_TOPIC`, `KAFKA_GROUP` | wallet / notification / analytics | `localhost:9092`, `wallet.transactions` |
| `MONGO_URI`, `MONGO_DB`, `MONGO_COLLECTION` | notification | `mongodb://localhost:27017`, `notification`, `transfers` |
| `CLICKHOUSE_ADDR`, `CLICKHOUSE_DB` | analytics | `localhost:9000`, `analytics` |

Секреты (`JWT_SECRET`, пароли БД, пароль ClickHouse) передаются через переменные
окружения и не хранятся в репозитории в боевой конфигурации.

## Надёжность и обработка данных

- **Миграции** применяются при старте сервисов: SQL-файлы для PostgreSQL,
  DDL для ClickHouse, индексы для MongoDB. Повторный запуск безопасен.
- **Kafka — at-least-once.** Идемпотентность приёмников: уникальный индекс
  `transaction_id` в MongoDB и `ReplacingMergeTree` + `FINAL` в ClickHouse.
- **Ретраи и DLQ.** `gw-notification` повторяет запись в Mongo с экспоненциальной
  паузой, после чего отправляет сообщения в `wallet.transactions.dlq`.
  `gw-analytics` после ретраев дропает батч, чтобы не блокировать партицию;
  потери видны в счётчике `dropped`.
- **Недоступность зависимостей не роняет сервис зря**: недоступная Kafka не мешает
  HTTP-операциям кошелька (событие теряется с записью в лог), клиент курсов
  подключается лениво, а `gw-notification`/`gw-analytics` переживают перезапуск
  брокеров и продолжают с сохранённых offset'ов.
- **Graceful shutdown** по `SIGINT`/`SIGTERM`: серверы перестают принимать запросы,
  консьюмеры досылают текущий батч и коммитят смещения, затем закрываются пулы.
- **Обмен валют** выполняется одной транзакцией БД: `SELECT … FOR UPDATE` по обеим
  строкам баланса в детерминированном порядке, проверка средств, два `UPDATE` и
  запись в историю; `CHECK (amount >= 0)` — дополнительная защита от отрицательного
  баланса.

## Наблюдаемость

- Логи в JSON (`log/slog`): уровень из `LOG_LEVEL`, поля `service`, `request_id`,
  `user_id`, `transaction_id`, `method`/`path`/`status`/`duration_ms` для запросов.
- `request-id` проходит через HTTP-заголовок `X-Request-ID` и метаданные gRPC,
  поэтому запрос клиента прослеживается в обоих сервисах.
- Пробы: `/health` (процесс жив) и `/ready` (зависимость доступна) у HTTP-сервисов,
  `grpc.health.v1` у exchanger.
- Счётчики обработки (`/stats`): `received`, `stored`, `duplicates`/`skipped`,
  `poison`, `retries`, `dropped`, `committed`.
- Ручная проверка gRPC без внешних утилит: `go run ./cmd/grpcprobe -addr localhost:50051`
  (курсы, кросс-курс, `-health`).

## Тестирование

```bash
go test ./...                    # unit-тесты каждого модуля
```

Интеграционные тесты работают с настоящими хранилищами и включаются тегом
`integration`; без переменной окружения они пропускаются:

```bash
TEST_DATABASE_URL='postgres://wallet:wallet@localhost:5432/wallet?sslmode=disable' \
  go test -tags=integration ./...                # кошелёк: схема и атомарность балансов

TEST_MONGO_URI='mongodb://localhost:27017' \
  go test -tags=integration ./internal/storages/mongo   # notification: идемпотентность

TEST_CLICKHOUSE_ADDR='localhost:9000' \
  go test -tags=integration ./internal/storages/clickhouse   # analytics: агрегации и дедуп
```

Покрыты ключевые сценарии: регистрация/логин, пополнение/вывод/обмен, недостаток
средств, JWT (просрочка, чужой секрет), TTL-кэш курсов, порог публикации событий,
батчи/ретраи/DLQ консьюмеров, идемпотентность, агрегации и границы периодов.

## Структура репозитория

```
Wallet2/
├── go.work                    # рабочее пространство Go-модулей
├── Makefile                   # proto / build / test / vet / up / down
├── docker-compose.yml         # весь стек: 4 базы/брокера + 4 сервиса
├── deploy/
│   ├── postgres/init/         # вторая база (exchanger) при первом старте
│   └── mongo/init/            # индексы коллекции transfers
├── scripts/                   # dev-env.ps1, build.ps1, test.ps1, vet.ps1
├── proto-exchange/            # модуль github.com/proto-exchange/exchange_grpc
│   └── exchange/              # exchange.proto + сгенерированные стабы
├── gw-exchanger/              # gRPC-сервис курсов
├── gw-currency-wallet/        # REST API кошелька
├── gw-notification/           # Kafka → MongoDB: история крупных переводов
└── gw-analytics/              # Kafka → ClickHouse: метрики и агрегации
```

Каждый сервис — отдельный Go-модуль с `go.mod`, `Makefile`, `config.env`,
`Dockerfile` и README. Внутри — одинаковая схема:

```
cmd/main.go                  -c config.env -> config.LoadEnv -> logger -> app.New -> pkg/app.Run
internal/app/app.go          композиция компонентов: старт по порядку, стоп в обратном
internal/config/             Config + LoadEnv + значения по умолчанию
internal/logger/             slog: JSON, уровни DEBUG/INFO/WARN/ERROR, поле service
internal/storages/           интерфейс Storage + реализация под конкретную БД
internal/<транспорт>/        gRPC-сервер, HTTP-сервер, Kafka-продюсер/консьюмер
pkg/utils.go                 хелперы окружения (GetEnv, GetEnvInt, GetEnvDuration, GetEnvList)
pkg/app/app.go               Lifecycle-компоненты и graceful shutdown по SIGINT/SIGTERM
```

## Эксплуатация

- Образы собираются multi-stage и запускаются в distroless (`CGO_ENABLED=0`) от
  непривилегированного пользователя.
- Каждый сервис конфигурируется переменными окружения — один образ на любое
  окружение; `config.env` в образе служит значением по умолчанию.
- Консьюмеры масштабируются репликами в одной consumer group: число полезных
  реплик ограничено числом партиций топика (для 1000 msg/s рекомендуется ≥ 3).
- PostgreSQL/ClickHouse/MongoDB в боевой конфигурации требуют аутентификации и TLS;
  в `docker-compose.yml` приведена конфигурация для локального запуска.

## Ограничения и допущения

- Окна аналитики скользящие: `period=1d` — последние 24 часа, а не календарные сутки.
- Запросы аналитики используют `FINAL` ради точной дедупликации; при росте объёмов
  данные материализуются (`MATERIALIZED VIEW`).
- Ретеншн истории задаётся TTL-индексом в MongoDB и `TTL` в DDL ClickHouse; по
  умолчанию данные хранятся без ограничения срока.
- JWT не хранится на сервере: отзыв всех токенов выполняется сменой `JWT_SECRET`.
- Курсы валют поставляются только `gw-exchanger`; без него недоступны
  `/api/v1/exchange/rates` и `/api/v1/exchange`, остальные операции работают.
