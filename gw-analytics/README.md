# gw-analytics

Сервис аналитики: читает события из Kafka своей consumer group, складывает их в
ClickHouse и отдаёт метрики — количество по типам/статусам, задержку доставки,
частоту ошибок и агрегации за 1m/5m/1h/1d/1w.

## Стек

- Go 1.25, `segmentio/kafka-go` (consumer group, ручной commit)
- ClickHouse (`clickhouse-go/v2`), `ReplacingMergeTree` с дедупом по `event_id`
- `log/slog` (JSON-логи), HTTP API на стандартной библиотеке

## Запуск

```bash
make build                       # GOOS=linux GOARCH=amd64 go build -o main ./cmd
./main -c config.env
```

Полный стек (Postgres, Kafka, MongoDB, ClickHouse и четыре сервиса) поднимается
из корня репозитория:

```bash
docker compose up --build
```

При старте сервис сам создаёт базу `CLICKHOUSE_DB` и таблицу `events`
(DDL из `migrations/clickhouse/*.sql`).

## API

Порт `HTTP_PORT` (по умолчанию `:8083`), все ответы — JSON.

| Метод | Путь | Параметры | Назначение |
|---|---|---|---|
| GET | `/api/v1/analytics/summary` | `period` = `1m`/`5m`/`1h`/`1d`/`1w` (по умолчанию `5m`) | сводные метрики за последний период |
| GET | `/api/v1/analytics/timeseries` | `period`, `step` (по умолчанию `step` ≈ период/60, не меньше 10s) | метрики в бакетах |
| GET | `/health` | — | процесс жив |
| GET | `/ready` | — | 200, если ClickHouse доступен; иначе 503 |
| GET | `/stats` | — | счётчики консьюмера |

### `summary`

```bash
curl -s "localhost:8083/api/v1/analytics/summary?period=5m"
```

```json
{
  "period": "5m",
  "from": "2026-11-14T11:55:00Z",
  "to": "2026-11-14T12:00:00Z",
  "total": 120,
  "failed": 6,
  "error_rate": 0.05,
  "by_operation": { "DEPOSIT": 80, "WITHDRAW": 25, "EXCHANGE": 15 },
  "by_status": { "SUCCESS": 114, "FAILED": 6 },
  "latency": { "avg_ms": 12.4, "p95_ms": 38, "max_ms": 120 }
}
```

- окно — **последние** `period` до текущего момента: `[now-period, now)`;
- `latency` — от `created_at` (время операции) до `received_at` (когда событие
  получила аналитика), то есть задержка доставки;
- `error_rate` = `failed / total` (0, если событий не было).

### `timeseries`

```bash
curl -s "localhost:8083/api/v1/analytics/timeseries?period=5m&step=30s"
```

```json
{
  "period": "5m",
  "step": "30s",
  "from": "2026-11-14T11:55:00Z",
  "to": "2026-11-14T12:00:00Z",
  "points": [
    { "bucket": "2026-11-14T11:55:00Z", "total": 20, "failed": 1, "latency_avg_ms": 11.2 }
  ]
}
```

Ограничения шага: от `1s` до `period`, иначе 400. Пустой `points` возвращается как `[]`.

## Событие в Kafka

Формат тот же, что публикует кошелёк (см. README `gw-currency-wallet`).
Обязательные поля для аналитики: `transaction_id`, `user_id`, `operation`,
`status`, `created_at`. В отличие от `gw-notification`, здесь сохраняются
**и `SUCCESS`, и `FAILED`** — иначе не посчитать частоту ошибок.

Сервис добавляет к событию `received_at` и `latency_ms`.

## Данные в ClickHouse

```sql
CREATE TABLE analytics.events (
    event_id String, transaction_id String, user_id String,
    operation LowCardinality(String), status LowCardinality(String),
    from_currency LowCardinality(String), to_currency LowCardinality(String),
    amount Float64, rate Float64, result_amount Float64, rub_equivalent Float64,
    created_at DateTime64(3, 'UTC'), received_at DateTime64(3, 'UTC'),
    latency_ms Int64, source LowCardinality(String), schema_version Int32
)
ENGINE = ReplacingMergeTree(received_at)
PARTITION BY toYYYYMM(created_at)
ORDER BY event_id;
```

- `ORDER BY event_id` + `ReplacingMergeTree` → повторная вставка того же события
  схлопывается при merge; запросы используют `FINAL`, чтобы дедуп применялся сразу;
- `PARTITION BY toYYYYMM(created_at)` → окна `1d`/`1w` читают меньше партиций;
- `LowCardinality` для строковых справочников (`operation`, `status`, валюты) —
  экономия места и ускорение группировок.

## Обработка сообщений

```
FetchMessage → батч до 1000 или 500 мс → decode/validate → InsertBatch → CommitMessages
                                            │
                                            └ невалидное → пропускаем (poison), но коммитим
```

| Правило | Зачем |
|---|---|
| Батч до `KAFKA_BATCH_SIZE` (1000) | один `INSERT` вместо сотен — так держится 1000 msg/s |
| Ручной commit после вставки | at-least-once: при падении батч перечитается, дубли схлопнет `ReplacingMergeTree` |
| Ретраи с backoff (`KAFKA_MAX_RETRIES`, `KAFKA_RETRY_BACKOFF`) | пережить короткий сбой ClickHouse |
| После ретраев батч **дропается** и коммитится | аналитика — производные данные: лучше потерять батч, чем заблокировать партицию навсегда; потери видны в `stats.dropped` |
| `Stop` досылает текущий батч | при остановке не теряются уже прочитанные сообщения |
| Невалидные сообщения пропускаются | «ядовитое» сообщение не должно останавливать поток |

## Переменные окружения

| Переменная | По умолчанию | Назначение |
|---|---|---|
| `HTTP_PORT` | `8083` | порт HTTP API |
| `SHUTDOWN_TIMEOUT` | `20s` | таймаут graceful shutdown |
| `KAFKA_BROKERS` | `localhost:9092` | брокеры |
| `KAFKA_TOPIC` | `wallet.transactions` | топик событий |
| `KAFKA_GROUP` | `gw-analytics` | consumer group (независимая от notification) |
| `KAFKA_BATCH_SIZE` | `1000` | размер батча |
| `KAFKA_POLL_TIMEOUT` | `500ms` | сколько ждать сообщение перед сбросом батча |
| `KAFKA_COMMIT_TIMEOUT` | `5s` | таймаут commit |
| `KAFKA_MAX_RETRIES` | `5` | попыток записи в ClickHouse |
| `KAFKA_RETRY_BACKOFF` | `200ms` | начальная пауза, дальше ×2 |
| `CLICKHOUSE_ADDR` | `localhost:9000` | native-протокол |
| `CLICKHOUSE_DB` | `analytics` | база (создаётся при старте) |
| `CLICKHOUSE_USER` | `default` | пользователь |
| `CLICKHOUSE_PASSWORD` | — | пароль |
| `CLICKHOUSE_TIMEOUT` | `5s` | таймаут подключения |
| `CLICKHOUSE_MIGRATIONS` | `migrations/clickhouse` | каталог DDL |
| `LOG_LEVEL` | `info` | `debug`, `info`, `warn`, `error` |

## Устройство

```
cmd/main.go                     -c config.env -> config.LoadEnv -> logger -> app.New -> pkg/app.Run
internal/app/app.go             композиция: clickhouse -> kafka-consumer -> http-server
internal/config/                Config, LoadEnv, значения по умолчанию
internal/model/event.go         событие, валидация, latency, структуры ответов
internal/aggregator/            разбор периодов/шагов, окно [from, to)
internal/consumer/              цикл чтения, батчи, ретраи, счётчики
internal/storages/              интерфейс Storage
internal/storages/clickhouse/   подключение, DDL, вставка, SQL-агрегации
internal/httpserver/            HTTP API, пробы, счётчики
migrations/clickhouse/          DDL таблицы events
pkg/utils.go, pkg/app/          env-хелперы и graceful shutdown
```

## Тесты

```bash
go test ./...
```

| Пакет | Что проверяется |
|---|---|
| `internal/aggregator` | разбор периодов (`1m…1w` и мусор), выбор шага, границы окна |
| `internal/consumer` | батч сохраняется и коммитится, `FAILED` тоже пишется, poison пропускается, после ретраев батч дропается и коммитится, `Stop` досылает батч |
| `internal/httpserver` | `summary`/`timeseries` (коды 200/400/500, период по умолчанию, окно, шаг), `/health`, `/ready`, `/stats` |

Интеграционные тесты (настоящий ClickHouse) — под тегом `integration`:

```bash
TEST_CLICKHOUSE_ADDR='localhost:9000' go test -tags=integration ./internal/storages/clickhouse
```

Без `TEST_CLICKHOUSE_ADDR` они пропускаются. Проверяют создание схемы, вставку,
агрегации (`summary`, `timeseries`) и дедупликацию повторного `event_id`.

## Ограничения и допущения

- Окна считаются от текущего момента (`[now-period, now)`), а не по календарным
  границам: «за 1d» — последние 24 часа.
- `FINAL` в запросах точный, но дороже: на больших объёмах имеет смысл
  периодическая материализация (`MATERIALIZED VIEW`) — в текущем объёме не требуется.
- DLQ не используется намеренно: после исчерпания ретраев батч дропается (см. таблицу выше), чтобы не блокировать партицию; для производных данных это осознанный компромисс.
- Ретеншн задаётся `TTL` в DDL таблицы; по умолчанию данные хранятся без ограничения срока.
- Метрика ретраев ведётся в каждом консьюмере и видна в `/stats`; в отчёт достаточно
  брать один источник — логика у консьюмеров одинаковая. В оконные агрегации она не
  включена: счётчик накопительный с момента старта.
