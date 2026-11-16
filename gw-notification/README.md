# gw-notification

Сервис истории крупных переводов: читает события из Kafka (`wallet.transactions`),
сохраняет успешные операции в MongoDB и ведёт счётчики обработки.
Целевая производительность — до 1000 сообщений в секунду.

## Стек

- Go 1.25, `segmentio/kafka-go` (consumer group, ручной commit)
- MongoDB (`go.mongodb.org/mongo-driver`), уникальный индекс по `transaction_id`
- `log/slog` (JSON-логи), служебный HTTP-сервер для проб и статистики

## Запуск

```bash
make build                       # GOOS=linux GOARCH=amd64 go build -o main ./cmd
./main -c config.env
```

Полный стек (Postgres, Kafka, MongoDB, exchanger, кошелёк и этот сервис) поднимается
из корня репозитория:

```bash
docker compose up --build
```

Служебные ручки на `HTTP_PORT` (по умолчанию `:8082`):

| Метод | Путь | Что отдаёт |
|---|---|---|
| GET | `/health` | `{"status":"ok"}` — процесс жив |
| GET | `/ready` | 200, если MongoDB доступна; 503 с текстом ошибки, если нет |
| GET | `/stats` | счётчики: `received`, `stored`, `duplicates`, `skipped`, `poison`, `retries`, `dlq`, `committed` |

Счётчик `retries` — источник метрики ретраев; достаточно вести её в одном сервисе,
логика у консьюмеров одинаковая.

## Событие в Kafka

Сервис ожидает JSON, который публикует кошелёк при операциях с рублёвым
эквивалентом от 30 000:

```json
{
  "event_id": "uuid",
  "transaction_id": "uuid",
  "user_id": "uuid",
  "operation": "DEPOSIT|WITHDRAW|EXCHANGE",
  "status": "SUCCESS|FAILED",
  "from_currency": "USD",
  "to_currency": "RUB",
  "amount": 31000,
  "rate": 90,
  "result_amount": 31000,
  "rub_equivalent": 2790000,
  "created_at": "2026-11-01T10:00:00.000Z",
  "source": "gw-currency-wallet",
  "schema_version": 1
}
```

Что сервис делает с полями:

- **обязательные**: `transaction_id`, `user_id`, `operation`, `status` (`SUCCESS`/`FAILED`), `created_at`;
- в историю попадают **только `SUCCESS`** — крупные переводы; `FAILED` пропускаются
  (они нужны аналитике, а не истории переводов) и учитываются в счётчике `skipped`;
- `event_id` при отсутствии подставляется из `transaction_id`;
- к документу добавляются `received_at` (время получения) и `latency_ms`
  (сколько миллисекунд прошло от `created_at` до получения сервисом).

## Как устроена обработка

```
FetchMessage → накопление батча → разбор → InsertMany → CommitMessages
                     │                            │
                     │                            └─ дубликаты считаются, но не ошибка
                     └─ «ядовитое» сообщение → DLQ → commit (чтобы не залипало)
```

| Правило | Зачем |
|---|---|
| Батч до `KAFKA_BATCH_SIZE` (500) или по таймауту `KAFKA_POLL_TIMEOUT` | меньше запросов в Mongo, выше пропускная способность |
| `InsertMany` с `ordered=false` + уникальный индекс `transaction_id` | идемпотентность: дубль из Kafka не создаёт второй документ |
| Ручной commit **после** записи (`CommitInterval: 0`) | at-least-once: при падении сообщение будет прочитано снова, дубль отсеет индекс |
| Ретраи с экспоненциальной паузой (`KAFKA_MAX_RETRIES`, `KAFKA_RETRY_BACKOFF`) | пережить короткие сбои Mongo |
| DLQ `wallet.transactions.dlq` | «ядовитые» сообщения и батчи, которые не удалось записать, не блокируют поток |
| `Stop` отменяет цикл и **досылает текущий батч** | при остановке не теряются уже прочитанные сообщения |
| Структурные логи с `transaction_id` (в ошибках разбора) и счётчиками батча | видно, что и почему произошло |

Дубликаты определяются по коду ошибки MongoDB `11000` (нарушение уникального
индекса) и попадают в счётчик `duplicates`, а не в ошибки.

## Переменные окружения

| Переменная | По умолчанию | Назначение |
|---|---|---|
| `HTTP_PORT` | `8082` | порт служебного HTTP-сервера |
| `SHUTDOWN_TIMEOUT` | `20s` | таймаут graceful shutdown |
| `KAFKA_BROKERS` | `localhost:9092` | брокеры (через запятую) |
| `KAFKA_TOPIC` | `wallet.transactions` | топик событий |
| `KAFKA_GROUP` | `gw-notification` | consumer group |
| `KAFKA_DLQ_TOPIC` | `wallet.transactions.dlq` | топик «мёртвых» сообщений |
| `KAFKA_BATCH_SIZE` | `500` | размер батча |
| `KAFKA_POLL_TIMEOUT` | `500ms` | сколько ждать сообщение перед сбросом батча |
| `KAFKA_COMMIT_TIMEOUT` | `5s` | таймаут commit и записи в DLQ |
| `KAFKA_MAX_RETRIES` | `5` | попыток записи батча в Mongo |
| `KAFKA_RETRY_BACKOFF` | `200ms` | начальная пауза между попытками (дальше ×2) |
| `MONGO_URI` | `mongodb://localhost:27017` | подключение к MongoDB |
| `MONGO_DB` | `notification` | база |
| `MONGO_COLLECTION` | `transfers` | коллекция |
| `MONGO_CONNECT_TIMEOUT` | `5s` | таймаут подключения |
| `LOG_LEVEL` | `info` | `debug`, `info`, `warn`, `error` |

## Устройство

```
cmd/main.go                    -c config.env -> config.LoadEnv -> logger -> app.New -> pkg/app.Run
internal/app/app.go            композиция: mongo -> kafka-consumer -> http-server
internal/config/               Config, LoadEnv, значения по умолчанию
internal/model/event.go        событие, валидация, расчёт latency_ms
internal/consumer/             цикл чтения, батчи, ретраи, DLQ, счётчики
internal/storages/             интерфейс Storage + InsertResult
internal/storages/mongo/       подключение, индексы, пакетная вставка
internal/httpserver/           /health, /ready, /stats
pkg/utils.go, pkg/app/         env-хелперы и graceful shutdown
```

Порядок старта: MongoDB (создаёт индексы) → консьюмер → HTTP-сервер.
Остановка — в обратном порядке: сервер перестаёт отвечать, консьюмер досылает
батч и коммитит, затем закрывается подключение к Mongo.

## Тесты

```bash
go test ./...
```

Покрыто (Kafka и Mongo подменены интерфейсами):

| Тест | Что проверяет |
|---|---|
| `TestBatchStoredAndCommitted` | батч сохраняется целиком, сообщения коммитятся, клиенты закрываются |
| `TestPoisonMessageGoesToDLQ` | невалидный JSON уходит в DLQ, валидное сообщение сохраняется, коммитятся оба |
| `TestFailedStatusSkipped` | событие со `FAILED` не пишется в историю, но коммитится |
| `TestStorageFailureGoesToDLQAfterRetries` | после `KAFKA_MAX_RETRIES` батч уходит в DLQ, сообщение не залипает |
| `TestDuplicatesCounted` | дубликаты считаются отдельно и не считаются ошибкой |
| `TestStopFlushesPendingBatch` | при остановке текущий батч сохраняется и коммитится |
| `/health`, `/ready`, `/stats` | 200/503 в зависимости от доступности Mongo, корректный JSON счётчиков |

## Ограничения и допущения

- В локальной конфигурации MongoDB работает без аутентификации; в боевой включаются пользователь и TLS.
- Ретеншн задаётся TTL-индексом по `created_at`; по умолчанию документы хранятся без ограничения срока.
- 1000 msg/s достигается при достаточном числе партиций топика: одна партиция
  обрабатывается одним консьюмером, поэтому для масштабирования нужно ≥ 3 партиций
  и несколько реплик сервиса в одной consumer group.
