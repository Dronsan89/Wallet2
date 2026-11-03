# gw-currency-wallet

Кошелёк-обменник: регистрация и авторизация по JWT, баланс в USD/RUB/EUR,
пополнение, вывод средств и обмен валют. Курсы валют получает по gRPC у сервиса
`exchange` (репозиторий `proto-exchange`) и кэширует их на короткое время.
Крупные операции публикуются в Kafka для сервисов истории и аналитики.

Подробный разбор устройства «для новичка» (что за файл, как проходит запрос,
почему сделано именно так) — в [docs/WALKTHROUGH.md](docs/WALKTHROUGH.md).

## Стек

- Go 1.25, Gin (HTTP), JWT HS256 (`golang-jwt/jwt/v5`), bcrypt
- PostgreSQL 16 (`pgx/v5` + `pgxpool`), миграции применяются при старте
- gRPC-клиент курсов (`proto-exchange`), TTL-кэш курсов
- Kafka (`segmentio/kafka-go`), `log/slog` (JSON-логи), OpenAPI 3.0.3

## Запуск

```bash
make build                       # GOOS=linux GOARCH=amd64 go build -o main ./cmd
./main -c config.env
```

Сервис слушает `HTTP_PORT` (по умолчанию `:8080`), миграции из `MIGRATIONS_PATH`
применяются автоматически при старте.

Локальный стенд с базой и Kafka (нужен Docker и доступ к образам):

```bash
docker compose up --build
```

Сервиса курсов в стенде нет: без него отвечают все ручки, кроме
`GET /api/v1/exchange/rates` и `POST /api/v1/exchange`.

## API

Все ответы — JSON, ошибки `{"error": "..."}`. Защищённые ручки требуют
`Authorization: Bearer <JWT>`; токен выдаёт `POST /api/v1/login`.

| Метод | Путь | Тело | Успех | Ошибка |
|---|---|---|---|---|
| POST | `/api/v1/register` | `username`, `password`, `email` | 201 `{"message":"User registered successfully"}` | 400 `Username or email already exists` |
| POST | `/api/v1/login` | `username`, `password` | 200 `{"token":"<JWT>"}` | 401 `Invalid username or password` |
| GET | `/api/v1/balance` | — | 200 `{"balance":{"USD":100,"RUB":0,"EUR":0}}` | 401 |
| POST | `/api/v1/wallet/deposit` | `amount`, `currency` | 200 `{"message":"Account topped up successfully","new_balance":{...}}` | 400 `Invalid amount or currency` |
| POST | `/api/v1/wallet/withdraw` | `amount`, `currency` | 200 `{"message":"Withdrawal successful","new_balance":{...}}` | 400 `Insufficient funds or invalid amount` |
| GET | `/api/v1/exchange/rates` | — | 200 `{"rates":{"USD":90,"RUB":1,"EUR":100}}` | 500 `Failed to retrieve exchange rates` |
| POST | `/api/v1/exchange` | `from_currency`, `to_currency`, `amount` | 200 `{"message":"Exchange successful","exchanged_amount":9,"new_balance":{...}}` | 400 `Insufficient funds or invalid currencies` |

Дополнительно: `GET /health`, `GET /swagger/doc.json` (OpenAPI-спецификация),
`GET /swagger/` (страница со списком ручек).

### Пример сценария

```bash
curl -X POST http://localhost:8080/api/v1/register \
  -H 'Content-Type: application/json' \
  -d '{"username":"alice","password":"secret123","email":"alice@example.com"}'

TOKEN=$(curl -s -X POST http://localhost:8080/api/v1/login \
  -H 'Content-Type: application/json' \
  -d '{"username":"alice","password":"secret123"}' | sed -E 's/.*"token":"([^"]+)".*/\1/')

curl -X POST http://localhost:8080/api/v1/wallet/deposit \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"amount":100,"currency":"USD"}'

curl -H "Authorization: Bearer $TOKEN" http://localhost:8080/api/v1/exchange/rates

curl -X POST http://localhost:8080/api/v1/exchange \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"from_currency":"USD","to_currency":"EUR","amount":10}'

curl -X POST http://localhost:8080/api/v1/wallet/withdraw \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"amount":10,"currency":"USD"}'

curl -H "Authorization: Bearer $TOKEN" http://localhost:8080/api/v1/balance
```

## Курсы и кэш

Курс от сервиса курсов — «сколько рублей стоит одна единица валюты»
(USD 90, EUR 100, RUB 1). Кросс-курс считается как `rate(from) / rate(to)`.

Ручка `/api/v1/exchange/rates` наполняет TTL-кэш (`RATES_CACHE_TTL`, по умолчанию
5 с), а `/api/v1/exchange` берёт курс из кэша, если он свежий, иначе делает
gRPC-вызов. Попадания и промахи кэша видны в логах (`debug`).

## Крупные операции и Kafka

Операция с рублёвым эквивалентом `>= BIG_TRANSFER_THRESHOLD` (по умолчанию 30 000)
публикуется в топик `wallet.transactions` (ключ — `user_id`):

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
  "created_at": "2026-10-30T07:20:00.123Z",
  "source": "gw-currency-wallet",
  "schema_version": 1
}
```

Публикуются и `FAILED`-операции (например, списание при недостатке средств) —
иначе аналитика не посчитает частоту ошибок. Недоступность Kafka не ломает
HTTP-операцию: ошибка пишется в лог, ответ клиенту не зависит от брокера.

## Переменные окружения

| Переменная | По умолчанию | Назначение |
|---|---|---|
| `HTTP_PORT` | `8080` | порт HTTP-сервера |
| `SHUTDOWN_TIMEOUT` | `15s` | таймаут graceful shutdown |
| `MIGRATIONS_PATH` | `migrations` | каталог SQL-миграций |
| `DATABASE_URL` | — | DSN Postgres (приоритетнее `DB_*`) |
| `DB_HOST` / `DB_PORT` / `DB_USER` / `DB_PASSWORD` / `DB_NAME` / `DB_SSLMODE` | `localhost` / `5432` / `wallet` / `wallet` / `wallet` / `disable` | параметры подключения |
| `DB_MAX_CONNS` / `DB_MIN_CONNS` | `20` / `5` | размер пула |
| `JWT_SECRET` | `dev-secret-change-me` | секрет подписи токенов |
| `JWT_TTL` | `24h` | время жизни токена |
| `EXCHANGER_GRPC_ADDR` | `localhost:50051` | адрес сервиса курсов |
| `EXCHANGER_TIMEOUT` | `2s` | таймаут gRPC-вызова |
| `RATES_CACHE_TTL` | `5s` | время жизни кэша курсов |
| `KAFKA_BROKERS` | `localhost:9092` | брокеры (через запятую) |
| `KAFKA_TOPIC` | `wallet.transactions` | топик событий |
| `BIG_TRANSFER_THRESHOLD` | `30000` | порог крупной операции в рублях |
| `LOG_LEVEL` | `info` | `debug`, `info`, `warn`, `error` |

## Устройство

```
cmd/main.go                 -c config.env -> config.LoadEnv -> logger -> app.New -> pkg/app.Run
internal/app/app.go         сборка зависимостей, порядок старта и остановки компонентов
internal/config/            Config, LoadEnv, значения по умолчанию
internal/storages/          интерфейс Storage + model (User, Transaction, Balances)
internal/storages/postgres/ пул pgx, миграции, атомарные операции над балансами
internal/auth/              bcrypt и JWT (HS256)
internal/rates/             gRPC-клиент курсов и TTL-кэш
internal/kafka/             продюсер событий
internal/service/           бизнес-логика: регистрация, логин, депозит, вывод, обмен
internal/handler/           HTTP-слой (Gin): роутер, middleware, ручки, DTO
internal/server/            HTTP-сервер с graceful shutdown
docs/                       OpenAPI 3.0.3 (swagger.json / swagger.yaml), встроен через embed
migrations/                 SQL-миграции (применяются при старте)
tests/                      тесты сервисного и HTTP-уровня на фейках
```

Обмен выполняется одной транзакцией БД: `SELECT ... FOR UPDATE` по обеим строкам
баланса в порядке кода валюты, проверка средств, два `UPDATE` и запись в
`transactions`. Ограничение `CHECK (amount >= 0)` в схеме — дополнительная защита
от отрицательного баланса.

## Тесты

```bash
go test ./...
```

Покрыто: регистрация/логин (включая дубли и неверный пароль), пополнение и вывод,
обмен по кросс-курсу, недостаток средств, недоступность курсов, порог публикации
событий, `FAILED`-события, TTL-кэш курсов, JWT (срок действия, чужой секрет),
bcrypt, а также полный HTTP-сценарий от регистрации до баланса.

## Ограничения

- Курсы приходят только от сервиса курсов: если он недоступен, `/exchange/rates`
  и `/exchange` отвечают 500, остальные операции работают.
- Денежные суммы в JSON — числа (`float64`), в базе — `NUMERIC(20,2)`;
  расчёты ведутся в `decimal` с округлением half-up до двух знаков.
- Токен не отзывается: смена `JWT_SECRET` инвалидирует все выданные токены.
