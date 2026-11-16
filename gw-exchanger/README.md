# gw-exchanger

gRPC-сервис курсов валют: единственный источник курсов USD/RUB/EUR для
кошелька (`gw-currency-wallet`). Курсы лежат в PostgreSQL, доступ к ним идёт
через интерфейс `internal/storages.Storage` — базу можно заменить, не меняя
gRPC-слой (требование ТЗ).

## Как запустить

```bash
make build                       # GOOS=linux GOARCH=amd64 go build -o main ./cmd
./main -c config.env
```

Сервис слушает `GRPC_PORT` (по умолчанию `:50051`). Миграции из `MIGRATIONS_PATH`
применяются при старте и сразу засевают курсы: USD 90, EUR 100, RUB 1.

Полный стек (Postgres + Kafka + exchanger + кошелёк) поднимается из корня репозитория:

```bash
docker compose up --build
```

## Контракт

Курсы отдаются в единицах «сколько рублей стоит одна единица валюты» (`RUB = 1`).
Кросс-курс считается как `rub(from) / rub(to)` и округляется до 6 знаков.

| Метод | Запрос | Ответ |
|---|---|---|
| `ExchangeService/GetExchangeRates` | `Empty` | `map<string, float> rates` — курсы всех валют |
| `ExchangeService/GetExchangeRateForCurrency` | `from_currency`, `to_currency` | `from_currency`, `to_currency`, `rate` |

Ошибки:

| Код | Когда |
|---|---|
| `InvalidArgument` | неизвестная валюта (не USD/RUB/EUR) или пары нет в хранилище |
| `Internal` | ошибка БД |

Дополнительно зарегистрированы `grpc.health.v1.Health` и reflection — для
healthcheck'ов compose и ручных проверок.

## Проверка без grpcurl

`grpcurl` в окружении нет, поэтому в сервисе есть свой маленький клиент
`cmd/grpcprobe` (он же используется как healthcheck в compose):

```bash
go run ./cmd/grpcprobe -addr localhost:50051                 # все курсы
go run ./cmd/grpcprobe -addr localhost:50051 -from USD -to EUR  # кросс-курс
go run ./cmd/grpcprobe -addr localhost:50051 -health         # health-статус
```

Ожидаемый вывод:

```
курсы (рублей за 1 единицу валюты):
  EUR: 100
  RUB: 1
  USD: 90
```

Поле `x-request-id` можно передать в метаданных gRPC — он попадёт в логи вызова.

## Переменные окружения

| Переменная | По умолчанию | Назначение |
|---|---|---|
| `GRPC_PORT` | `50051` | порт gRPC-сервера |
| `SHUTDOWN_TIMEOUT` | `15s` | таймаут graceful shutdown |
| `MIGRATIONS_PATH` | `migrations` | каталог SQL-миграций |
| `DATABASE_URL` | — | DSN Postgres (приоритетнее `DB_*`) |
| `DB_HOST` / `DB_PORT` / `DB_USER` / `DB_PASSWORD` / `DB_NAME` / `DB_SSLMODE` | `localhost` / `5432` / `exchanger` / `exchanger` / `exchanger` / `disable` | подключение к БД |
| `DB_MAX_CONNS` / `DB_MIN_CONNS` | `10` / `2` | размер пула |
| `LOG_LEVEL` | `info` | `debug`, `info`, `warn`, `error` |

## Устройство

```
cmd/main.go                  -c config.env -> config.LoadEnv -> logger -> app.New -> pkg/app.Run
cmd/grpcprobe/main.go        клиент для проверки gRPC и health
internal/app/app.go          композиция: postgres -> grpc-server, остановка в обратном порядке
internal/config/             Config, LoadEnv, значения по умолчанию
internal/storages/           интерфейс Storage + model (Rates, CurrencyRate)
internal/storages/postgres/  пул pgx, миграции, выборка курсов
internal/grpcserver/         реализация ExchangeService, интерсепторы, health, reflection
pkg/utils.go                 хелперы окружения и работы с валютами
pkg/app/app.go               Lifecycle-компоненты и graceful shutdown
migrations/                  схема currency_rates + стартовые курсы
```

Особенности реализации:

- **Интерсепторы** (по порядку): `request-id` (из метаданных или новый UUID),
  логирование каждого вызова (`method`, `request_id`, `duration_ms`, `code`),
  recovery — паника в обработчике превращается в `Internal`, процесс живёт.
- **Кэша нет**: курсы читаются из БД на каждый вызов. Кэширование — обязанность
  потребителя (кошелёк держит TTL-кэш 5 с), так что exchanger всегда отдаёт
  актуальные значения.
- **Курсы — только чтение**: API записи не требуется по ТЗ, данные меняются через
  миграции/SQL. `INSERT ... ON CONFLICT DO NOTHING` не затирает изменённые вручную курсы.

## Тесты

```bash
go test ./...
```

Покрыто:

- `GetExchangeRates` — три валюты, значения курсов; ошибка БД отдаётся как `Internal`;
- `GetExchangeRateForCurrency` — USD→EUR = 0.9, USD→RUB = 90, EUR→USD = 1.111111,
  одинаковые валюты дают 1, регистр не важен; неизвестная валюта (`GBP`, пустая) — `InvalidArgument`;
- `crossRate` — отсутствующая валюта в наборе курсов;
- **bufconn-тест**: настоящий gRPC-сервер и клиент по сгенерированным стабам —
  проверяет регистрацию сервиса, интерсепторы и health-статус `SERVING` без сети и портов.
