# gw-currency-wallet: как всё устроено

Подробный разбор сервиса «для новичка»: что за файл, зачем он нужен, как данные
проходят от HTTP-запроса до базы и обратно. Если нужно быстро запустить — смотрите
[README.md](../README.md), здесь — устройство и логика.

---

## 1. Какую задачу решает сервис

По ТЗ это «кошелёк-обменник»:

- пользователь **регистрируется** и **логинится** (получает JWT-токен);
- у него есть **баланс в трёх валютах**: USD, RUB, EUR;
- баланс можно **пополнить** и **вывести** средства;
- можно **посмотреть курсы** и **обменять** одну валюту на другую;
- **крупные** операции (от 30 000 ₽ в эквиваленте) должны уходить в Kafka —
  чтобы другой сервис сохранил историю, а третий посчитал аналитику.

Курсы валют сервис сам не придумывает: он спрашивает их у отдельного сервиса
`exchange` по gRPC (контракт лежит в модуле `proto-exchange`).

---

## 2. Общая картина

```
клиент (curl/Postman)
   │  HTTP + JSON, заголовок Authorization: Bearer <JWT>
   ▼
gw-currency-wallet :8080
   ├── Gin-роутер ── middleware (request-id, recovery, логи, JWT)
   ├── handler  ──── разбор JSON, коды ответов
   ├── service  ──── бизнес-логика: проверки, расчёты, события
   │     ├── storages (интерфейс) ──► PostgreSQL (users, balances, transactions)
   │     ├── rates.Cache ──────────► gRPC exchanger (курсы) ──► TTL-кэш
   │     └── kafka.Producer ───────► Kafka (wallet.transactions)
   └── server ────── http.Server с graceful shutdown
```

Четыре внешних зависимости:

| Зависимость | Зачем | Что будет, если недоступна |
|---|---|---|
| PostgreSQL | пользователи, балансы, история операций | сервис не стартует (без базы работать нечем) |
| exchanger (gRPC) | курсы валют | `/exchange/rates` и `/exchange` отвечают 500, остальное работает |
| Kafka | события о крупных операциях | операция выполняется, событие теряется, в лог пишется ошибка |
| клиент | — | — |

Такой порядок «что важнее» — осознанное решение: база критична, курсы важны только
для обмена, Kafka — вспомогательная.

---

## 3. Как читать код: путь одного запроса

Возьмём самый сложный сценарий — обмен валют `POST /api/v1/exchange`.

### Шаг 1. Старт процесса (`cmd/main.go`)

```go
func main() {
	configPath := flag.String("c", "config.env", "путь к файлу конфигурации")
	flag.Parse()
	if err := run(*configPath); err != nil { ... os.Exit(1) }
}
```

`./main -c config.env` — ровно та команда, что требует ТЗ. Дальше `run`:

1. `config.LoadEnv(path)` — читает `config.env` в переменные окружения (`godotenv`).
2. `logger.New(...)` — создаёт JSON-логгер.
3. `config.New()` — собирает `Config` из переменных.
4. `pkgapp.Run(app.New(cfg, log), log)` — запускает приложение и ждёт сигнала остановки.

### Шаг 2. Сборка зависимостей (`internal/app/app.go`)

`App.Start` создаёт всё «взрослое» хозяйство:

```go
storage := postgres.New(a.cfg, a.log)            // база
ratesClient, _ := rates.NewClient(...)           // gRPC-клиент курсов
ratesCache := rates.NewCache(ratesClient, ttl)   // кэш поверх клиента
producer := kafka.New(...)                       // продюсер Kafka
authManager := auth.NewManager(secret, ttl)      // JWT
walletService := service.New(service.Deps{...})  // бизнес-логика
api := handler.New(walletService, authManager, a.log)
httpServer := server.New(":"+port, api.Router(), a.log)
```

Затем все они попадают в список компонентов и запускаются **по порядку**:
`postgres → rates-client → kafka-producer → http-server`. Останавливаются —
**в обратном порядке** (сначала сервер, потом зависимости). Это гарантирует, что
сервер не примет запрос, когда база уже закрыта.

### Шаг 3. HTTP: роутер и middleware (`internal/handler/router.go`)

```go
router := gin.New()
// Порядок важен: request-id нужен всем, логирование стоит снаружи recovery,
// чтобы в access-лог попадал настоящий статус (500 после паники).
router.Use(requestIDMiddleware(), h.loggingMiddleware(), h.recoveryMiddleware())

v1 := router.Group("/api/v1")
v1.POST("/register", h.register)
v1.POST("/login", h.login)

authorized := v1.Group("", h.authMiddleware())
authorized.GET("/balance", h.balance)
authorized.POST("/wallet/deposit", h.deposit)
authorized.POST("/wallet/withdraw", h.withdraw)
authorized.GET("/exchange/rates", h.ratesHandler)
authorized.POST("/exchange", h.exchangeHandler)
```

Middleware — это «слои лука», через которые проходит каждый запрос (сверху вниз —
на входе, снизу вверх — на выходе):

| # | Middleware | Что делает |
|---|---|---|
| 1 | `requestIDMiddleware` | берёт `X-Request-ID` или генерирует UUID, кладёт в контекст и в ответ |
| 2 | `loggingMiddleware` | пишет JSON-лог запроса: метод, путь, статус, длительность, `user_id`. Лог пишется в `defer`, поэтому попадает в вывод всегда |
| 3 | `recoveryMiddleware` | ловит панику в обработчике → 500 `Internal server error`, процесс живёт |
| 4 | `authMiddleware` | только для защищённых ручек: проверяет JWT, достаёт `user_id` |

Почему `loggingMiddleware` стоит **снаружи** `recoveryMiddleware`: порядок выполнения
такой, что сначала отрабатывает внешний слой, а `c.Next()` уводит управление внутрь.
На панике внутренние слои разматываются первыми: recovery превращает панику в ответ
500, после чего управление возвращается в логирование, и `defer` пишет в лог уже
настоящий статус `500`. Если поставить логирование внутрь (после recovery), его
`defer` выполнился бы до того, как recovery выставит статус, и в логе оказался бы
неверный `200` — либо запись не появилась бы вовсе (так было до правки: код после
`c.Next()` не выполняется при панике).

### Шаг 4. Обработчик (`internal/handler/exchange.go`)

```go
func (h *Handler) exchangeHandler(c *gin.Context) {
	userID, ok := userIDFromContext(c)   // положил authMiddleware
	if !ok { 401 }

	var req exchangeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		400 "Insufficient funds or invalid currencies"
	}

	exchanged, balances, err := h.service.Exchange(ctx, userID, req.FromCurrency, req.ToCurrency, req.Amount)
	// err == nil                      -> 200 {"message":"Exchange successful", "exchanged_amount":…, "new_balance":…}
	// ErrInvalidInput/InsufficientFunds -> 400 "Insufficient funds or invalid currencies"
	// ErrRatesUnavailable             -> 500 "Failed to retrieve exchange rates"
	// прочее                          -> 500 "Internal server error"
}
```

Обработчик — «переводчик»: он не считает деньги, он только разбирает запрос,
зовёт сервис и превращает доменные ошибки в HTTP-коды. Здесь же добавляется
`request_id` в логи ошибок.

### Шаг 5. Бизнес-логика (`internal/service/exchange.go`)

```go
fromCurrency = pkg.NormalizeCurrency(fromCurrency)   // "usd" -> "USD"
amount = pkg.RoundMoney(amount)                      // 10.005 -> 10.01

if err := validateAmount(amount); err != nil { return ErrInvalidInput }        // > 0 и <= 1e9
if !pkg.IsSupportedCurrency(fromCurrency) || fromCurrency == toCurrency { ... } // только USD/RUB/EUR, разные

rate, err := s.crossRate(ctx, fromCurrency, toCurrency)   // 1 USD = 0.9 EUR
resultAmount := pkg.RoundMoney(amount.Mul(rate))          // 10 USD -> 9 EUR

tx := storages.Transaction{
	ID: uuid.New(), UserID: userID, Operation: "EXCHANGE", Status: "SUCCESS",
	FromCurrency: fromCurrency, ToCurrency: toCurrency,
	Amount: amount, ResultAmount: resultAmount, Rate: rate.Round(8),
	RubEquivalent: s.rubEquivalent(ctx, fromCurrency, amount),  // для порога Kafka
	CreatedAt: s.now(),
}

balances, err := s.storage.Exchange(ctx, tx)   // одна транзакция в БД
if errors.Is(err, storages.ErrInsufficientFunds) {
	tx.Status = "FAILED"
	s.publishEvent(ctx, tx)                     // аналитика увидит ошибку
	return ErrInsufficientFunds
}

s.publishEvent(ctx, tx)      // если RUB-эквивалент >= 30 000 — уйдёт в Kafka (в фоне)
return resultAmount, balances, nil
```

Ключевая мысль: **вся арифметика денег живёт в сервисе**, а хранилище умеет только
«атомарно применить операцию». Это позволяет подменить Postgres на другую БД,
не переписывая бизнес-логику (интерфейс `storages.Storage`).

### Шаг 6. Хранилище (`internal/storages/postgres/methods.go`)

Обмен — самая интересная часть. Нужно одновременно:

1. проверить, что денег хватает;
2. списать с одной валюты, начислить на другую;
3. не допустить гонки, если два обмена идут параллельно;
4. записать транзакцию в историю.

```go
tx, _ := c.pool.Begin(ctx)
defer tx.Rollback(ctx)                        // если что-то ниже упадёт — откат

// 1. Блокируем обе строки баланса в детерминированном порядке (по коду валюты),
//    чтобы два параллельных обмена не взяли блокировки в разном порядке (дедлок).
rows := tx.Query(`SELECT currency FROM balances
                   WHERE user_id = $1 AND currency = ANY($2)
                   ORDER BY currency FOR UPDATE`, userID, []string{from, to})
...
// 2. Списание: условие amount >= $1 выполняет сама БД — это атомарно.
tag := tx.Exec(`UPDATE balances SET amount = amount - $1::numeric
                 WHERE user_id = $2 AND currency = $3 AND amount >= $1::numeric`, ...)
if tag.RowsAffected() == 0 { return storages.ErrInsufficientFunds }

// 3. Начисление
tx.Exec(`UPDATE balances SET amount = amount + $1::numeric WHERE ...`)

// 4. История
tx.Exec(`INSERT INTO transactions (...) VALUES (...)`)
tx.Commit(ctx)
```

Почему именно так:

- `FOR UPDATE` — блокировка строки до конца транзакции. Пока один запрос меняет
  баланс, второй ждёт, а не читает устаревшее значение.
- `ORDER BY currency` — оба запроса блокируют строки в одинаковом порядке,
  значит не могут «заклинить» друг друга (классическая проблема дедлоков).
- `AND amount >= $1` в `UPDATE` — проверка и списание одним действием, окно
  для гонки отсутствует.
- `CHECK (amount >= 0)` в схеме — последний рубеж: даже если в коде появится
  ошибка, БД не даст уйти в минус.

---

## 4. Разбор по файлам

### `cmd/main.go` (39 строк)

Точка входа. Ничего, кроме: разобрать флаг `-c`, загрузить конфиг, создать логгер,
запустить приложение. Вся логика — в других пакетах (thin main — хорошая практика:
так проще тестировать).

### `internal/app/app.go` (122)

Композиция приложения. Здесь видно, **из чего состоит сервис**: база, клиент курсов,
кэш, продюсер Kafka, JWT-менеджер, сервис бизнес-логики, HTTP-слой, сервер.
Порядок старта/остановки и таймауты — тоже здесь.

### `internal/config/` (141)

- `config.go` — структура `Config` (порт, DSN, секрет JWT, адрес exchanger, Kafka,
  порог крупной операции) и функция `New()`, которая читает переменные окружения.
  `DATABASE_URL` приоритетнее отдельных `DB_*`.
- `defaults.go` — значения по умолчанию и таймауты приложения (30 с на старт, 15 с на стоп).
- `LoadEnv(path)` — читает `config.env`; **если файла нет, это не ошибка** (в контейнере
  переменные приходят из окружения).

### `pkg/utils.go` (121)

Мелкие помощники, которые не зависят от бизнес-логики:

| Функция | Зачем |
|---|---|
| `GetEnv`, `GetEnvInt`, `GetEnvBool`, `GetEnvDuration`, `GetEnvList`, `GetEnvDecimal` | чтение env с безопасным fallback (не паникуем на опечатке) |
| `NormalizeCurrency` | `" usd "` → `"USD"` |
| `IsSupportedCurrency` | проверка на USD/RUB/EUR |
| `RoundMoney` | округление half-up до 2 знаков |

### `internal/logger/logger.go` (32)

Обёртка над стандартным `log/slog`: JSON в stdout, уровень из `LOG_LEVEL`
(`debug/info/warn/error`), постоянное поле `service`. Каждая запись — одна строка
JSON, её удобно искать и парсить.

### `internal/server/server.go` (62)

`http.Server` с таймаутами (заголовки 5 с, чтение/запись 15 с, idle 60 с) и
`Shutdown(ctx)` — при остановке сервер сначала дожидается активных запросов,
потом закрывается. Это и есть graceful shutdown для HTTP-части.

### `pkg/app/app.go` (73)

Раннер приложения:

1. `Start(ctx)` с таймаутом (30 с);
2. ждём `SIGINT`/`SIGTERM` (в тестах сигнал подменяется каналом — `runWithSignals`);
3. `Stop(ctx)` с таймаутом (из конфига, 15 с).

Если `Start` упал — приложение не стартует (например, база недоступна).

### `internal/handler/` (434 строки)

| Файл | Что внутри |
|---|---|
| `router.go` | сборка маршрутов, `/health`, `/swagger/*`, конструктор `Handler` |
| `middleware.go` | request-id, JSON-логи, recovery |
| `auth.go` | JWT-middleware, `userIDFromContext`, ручки регистрации и логина |
| `wallet.go` | баланс, пополнение, списание, перевод баланса в JSON |
| `exchange.go` | курсы и обмен |
| `dto.go` | структуры запросов/ответов (формат JSON) |

DTO (Data Transfer Object) — это «форма» JSON. Например:

```go
type exchangeRequest struct {
	FromCurrency string          `json:"from_currency"`
	ToCurrency   string          `json:"to_currency"`
	Amount       decimal.Decimal `json:"amount"`
}
```

`Amount` — не `float64`, а `decimal.Decimal`: он умеет принять из JSON и число
(`10.5`), и строку (`"10.5"`), и не теряет копейки на округлениях.

### `internal/service/` (497 строк)

Сердце сервиса.

| Файл | Что делает |
|---|---|
| `service.go` | конструктор, расчёт кросс-курса, рублёвый эквивалент, публикация событий, валидаторы |
| `user.go` | регистрация (валидация → bcrypt → запись) и логин (поиск → сверка пароля → JWT) |
| `wallet.go` | баланс, пополнение, списание |
| `exchange.go` | курсы и обмен |
| `event.go` | структура события для Kafka и интерфейс `EventPublisher` |
| `errors.go` | доменные ошибки (`ErrInvalidInput`, `ErrInsufficientFunds`, …) |

Доменные ошибки — это «язык» сервиса. Обработчики не разбирают тексты ошибок,
а проверяют `errors.Is(err, service.ErrInsufficientFunds)` и выбирают HTTP-код.

### `internal/storages/` (453 строки)

| Файл | Что внутри |
|---|---|
| `storage.go` | интерфейс `Storage` (6 методов) и ошибки `ErrUserExists`, `ErrUserNotFound`, `ErrInsufficientFunds` |
| `model.go` | `User`, `Transaction`, `Balances` |
| `postgres/connector.go` | пул соединений (`pgxpool`), ping, применение миграций |
| `postgres/methods.go` | SQL-запросы: пользователь, балансы, депозит, вывод, обмен |
| `postgres/migrate.go` | мини-мигратор: читает `migrations/*.up.sql` и применяет неприменённые |

Интерфейс (`storage.go`) специально маленький:

```go
type Storage interface {
	CreateUser(ctx context.Context, username, email, passwordHash string) (User, error)
	UserByUsername(ctx context.Context, username string) (User, error)
	Balances(ctx context.Context, userID uuid.UUID) (Balances, error)
	Deposit(ctx context.Context, tx Transaction) (Balances, error)
	Withdraw(ctx context.Context, tx Transaction) (Balances, error)
	Exchange(ctx context.Context, tx Transaction) (Balances, error)
}
```

Требование ТЗ «описать интерфейс, чтобы можно было поменять БД» выполнено буквально:
сервисный слой видит только эти методы. В тестах вместо Postgres подставляется
`fakeStorage` из `tests/fakes_test.go` — тот же интерфейс, но данные в памяти.

Мини-мигратор работает так:

1. создаёт таблицу `schema_migrations` (какие миграции уже применены);
2. читает каталог `migrations/`, берёт `*.up.sql`, сортирует по имени;
3. для каждой неприменённой — выполняет файл в транзакции и записывает имя в
   `schema_migrations`.

Поэтому при старте сервиса схема всегда актуальна, а повторные запуски ничего не ломают.
Файлы читаются с диска (а не вшиваются в бинарь), потому что каталог `migrations`
копируется в Docker-образ отдельным слоем.

### `internal/rates/` (145 строк)

- `client.go` — gRPC-клиент: `GetExchangeRates` → `map[string]decimal.Decimal`.
  Соединение создаётся «лениво» (`grpc.NewClient`), поэтому сервис стартует,
  даже если exchanger ещё не поднялся.
- `cache.go` — TTL-кэш. Первый запрос идёт в exchanger, результат запоминается на
  `RATES_CACHE_TTL` (5 с). Пока запись свежая — отдаём из памяти, exchanger не
  тревожим. Ошибки **не кэшируются** (иначе после падения exchanger сервис
  залипнет на 5 секунд ошибок).

Счётчики `hits/misses` (атомарные) позволяют видеть в тестах и логах, что кэш реально
работает.

### `internal/kafka/producer.go` (54)

`kafka-go` Writer с `RequiredAcks: RequireAll` (ждём подтверждения всех реплик),
балансировщик `Hash` по ключу (события одного пользователя идут в одну партицию —
значит сохраняют порядок). Ключ — `user_id`, значение — JSON события.

### `internal/auth/` (107)

- `password.go` — bcrypt cost 12: в базе лежит только хеш, восстановить пароль нельзя.
- `jwt.go` — выпуск и проверка токена HS256. Внутри `Claims`: `user_id`, `username`,
  `sub`, `iat`, `exp`, `iss`. `Parse` отклоняет токен с чужим секретом, истёкшим
  сроком или неверным алгоритмом подписи (защита от подмены `alg`).

### `docs/` (17 + спека)

- `swagger.json` / `swagger.yaml` — OpenAPI 3.0.3 на все ручки: параметры, схемы,
  коды ответов, примеры.
- `docs.go` — вшивает оба файла в бинарь через `//go:embed` и отдаёт их наружу.

`swag` (генератор по аннотациям) в офлайн-окружении не собирается, поэтому спека
написана руками — она полная и валидная, просто поддерживается вручную.

### `migrations/` (2 файла)

`000001_init.up.sql` создаёт три таблицы:

```sql
users         id UUID PK, username UNIQUE, email UNIQUE, password_hash, created_at
balances      (user_id, currency) PK, amount NUMERIC(20,2) CHECK (amount >= 0), updated_at
transactions  id UUID PK, user_id, operation, status, from_currency, to_currency,
              amount, result_amount, rate, rub_equivalent, created_at
              + индекс (user_id, created_at DESC)
```

`000001_init.down.sql` — откат (drop в правильном порядке).

Почему `NUMERIC(20,2)`, а не `float`: `float` хранит двоичную дробь, и `0.1 + 0.2`
даёт `0.30000000000000004`. В деньгах это недопустимо.

### `tests/` (700 строк)

| Файл | Что проверяет |
|---|---|
| `fakes_test.go` | фейковое хранилище (в памяти), фейковые курсы, фейковый продюсер Kafka |
| `service_test.go` | бизнес-логику: регистрация/логин, депозит/вывод, обмен, порог событий, FAILED-события |
| `api_test.go` | HTTP-сценарий целиком: 201/200/400/401, тексты ошибок, `/health`, `/swagger/doc.json` |

Тесты работают без базы, Kafka и exchanger — именно благодаря интерфейсам
(`Storage`, `rates.Provider`, `service.EventPublisher`).

---

## 5. Данные и деньги

### Схема и связи

```
users 1 ──── N balances        (по строке на каждую валюту, создаются при регистрации)
users 1 ──── N transactions    (история операций)
```

Баланс — это **три строки** у пользователя: USD, RUB, EUR. При регистрации они
создаются с нулями, поэтому дальше операции — простые `UPDATE` без «вставь, если нет».

### Как считаются деньги

1. Пришло `10.005` → `RoundMoney` даёт `10.01` (half-up).
2. В БД лежит `10.01` как `NUMERIC(20,2)`.
3. Читаем обратно **строкой** (`amount::text`) и превращаем в `decimal.Decimal` —
   никаких потерь на `float64`.
4. В JSON отдаём число (`InexactFloat64()`), потому что ТЗ требует `"USD": 100.0`.
   Для отображения двух знаков этого достаточно.

### Кросс-курс

Курс от exchanger — «сколько рублей стоит единица валюты»: USD 90, EUR 100, RUB 1.

```
1 USD = 90 RUB
1 EUR = 100 RUB
1 USD = 90/100 = 0.9 EUR
```

`crossRate = rates[from] / rates[to]`, `result = amount * crossRate`, округление до 2 знаков.
Если какой-то валюты нет в ответе exchanger или знаменатель 0 — отвечаем 500
`Failed to retrieve exchange rates` (данные неполные, обмен делать нельзя).

### Рублёвый эквивалент

Нужен для порога «крупной операции»:

- для RUB: эквивалент = сумма;
- для USD/EUR: сумма × курс к рублю.

Если курсы недоступны, эквивалент считается нулевым, **операция не ломается** —
просто событие не уйдёт в Kafka (в лог пишется `warn`). Пользователь не должен
страдать из-за недоступности вспомогательного сервиса.

---

## 6. Ошибки и HTTP-коды

| Доменная ошибка | Где возникает | HTTP | Текст в ответе |
|---|---|---|---|
| `ErrInvalidInput` | сумма ≤ 0 или > 1e9, неизвестная валюта, обмен в ту же валюту, плохая регистрация | 400 | зависит от ручки (см. таблицу ниже) |
| `ErrUserExists` | дубль `username`/`email` | 400 | `Username or email already exists` |
| `ErrInvalidCredentials` | нет пользователя или неверный пароль | 401 | `Invalid username or password` |
| `ErrInsufficientFunds` | списание/обмен больше баланса | 400 | `Insufficient funds or invalid amount` / `Insufficient funds or invalid currencies` |
| `ErrRatesUnavailable` | exchanger недоступен | 500 | `Failed to retrieve exchange rates` |
| прочее | ошибки БД, паника и т.п. | 500 | `Internal server error` |

Тексты ошибок специально совпадают с ТЗ дословно — их сравнивает ревьюер и
проверяют тесты.

Ещё два важных случая:

- **нет токена / плохой токен** → 401 `Unauthorized` (это middleware, до обработчика);
- **невалидный JSON** → 400 с текстом, привязанным к ручке.

---

## 7. JWT: как это работает

1. `POST /api/v1/login` → сервис находит пользователя, сверяет bcrypt-хеш.
2. `auth.Manager.NewToken` собирает claims и подписывает их секретом `JWT_SECRET`:

```json
{ "user_id": "…", "username": "alice", "sub": "…", "iat": 1761…, "exp": 1762…, "iss": "gw-currency-wallet" }
```

3. Клиент присылает `Authorization: Bearer <token>`.
4. `authMiddleware` парсит и валидирует токен, кладёт `user_id` в контекст Gin.
5. Обработчик берёт `user_id` через `userIDFromContext` — **никогда** не доверяет
   `user_id`, присланному в теле запроса (иначе любой мог бы пополнить чужой счёт).

Токен stateless: сервер ничего не хранит, поэтому сервис легко масштабируется.
Обратная сторона — токен нельзя «отозвать», кроме смены секрета.

---

## 8. Kafka-события

Публикуются, когда `rub_equivalent >= BIG_TRANSFER_THRESHOLD` (30 000 по умолчанию).
Формат (`internal/service/event.go`):

```json
{
  "event_id": "uuid",
  "transaction_id": "uuid",
  "user_id": "uuid",
  "operation": "EXCHANGE",
  "status": "SUCCESS",
  "from_currency": "USD",
  "to_currency": "EUR",
  "amount": 100,
  "rate": 0.9,
  "result_amount": 90,
  "rub_equivalent": 9000,
  "created_at": "2026-11-01T10:00:00.000Z",
  "source": "gw-currency-wallet",
  "schema_version": 1
}
```

Тонкости:

- Публикуется **и `FAILED`** (например, попытка списать больше, чем есть). Так
  аналитика посчитает «частоту ошибок» — иначе откуда ей знать про отказы.
- Ключ сообщения — `user_id`: события одного пользователя идут в одну партицию,
  порядок сохраняется.
- Публикация — **в горутине** с таймаутом 2 с и контекстом, отвязанным от запроса
  (`context.WithoutCancel`). Смысл: клиенту не нужно ждать Kafka, а отмена
  HTTP-запроса не должна «убивать» уже начатую отправку.
- Ошибка публикации только логируется — операция считается успешной, потому что
  деньги уже изменились в БД.

---

## 9. Запуск и остановка

### Порядок старта

```
postgres (ping + миграции) → rates-client → kafka-producer → http-server
```

`http-server` поднимается последним: пока база не готова, принимать запросы нельзя.

### Порядок остановки

```
http-server (дожидается запросов) → kafka-producer → rates-client → postgres
```

### Что видно в логах

```json
{"level":"INFO","msg":"service configured","service":"gw-currency-wallet","http_port":"8080", …}
{"level":"INFO","msg":"migration applied","migration":"000001_init.up.sql"}
{"level":"INFO","msg":"postgres connected"}
{"level":"INFO","msg":"component started","component":"postgres"}
{"level":"INFO","msg":"http server started","addr":":8080"}
{"level":"INFO","msg":"application started","start_timeout":"30s"}
{"level":"INFO","msg":"http request","request_id":"…","method":"POST","path":"/api/v1/login","status":200,"duration_ms":42}
```

При остановке (Ctrl+C):

```json
{"level":"INFO","msg":"shutdown signal received","signal":"interrupt"}
{"level":"INFO","msg":"stopping component","component":"http-server"}
…
{"level":"INFO","msg":"application stopped"}
```

### Если базы нет

Сервис не стартует и честно об этом говорит:

```
gw-currency-wallet: app: start: start postgres: ping postgres: … connection refused
```

---

## 10. Тесты: как они устроены

Идея: подменить внешний мир интерфейсами.

| Интерфейс | Реальная реализация | Фейк в тестах |
|---|---|---|
| `storages.Storage` | `postgres.Connector` | `fakeStorage` (map в памяти) |
| `rates.Provider` | `rates.Client` / `rates.Cache` | `fakeRates` (курсы + счётчик вызовов) |
| `service.EventPublisher` | `kafka.Producer` | `fakePublisher` (канал событий) |

Что проверяется (25 тестов):

- регистрация: успех, дубль, короткий логин/пароль;
- логин: успех, неверный пароль, несуществующий пользователь;
- депозит/вывод: баланс меняется, неизвестная валюта и отрицательная сумма отклоняются,
  вывод больше баланса → `ErrInsufficientFunds`;
- обмен: кросс-курс, обмен в ту же валюту, недостаток средств, недоступность курсов;
- события: мелкая операция не публикует, крупная публикует с правильным статусом,
  неуспешный вывод публикует `FAILED`;
- кэш курсов: попадание, обновление по TTL, ошибки не кэшируются;
- JWT: round-trip, просроченный токен, чужой секрет;
- bcrypt: хеш не равен паролю, верный/неверный пароль;
- HTTP: полный сценарий регистрация → логин → депозит → курсы → обмен → вывод →
  баланс, проверка кодов и текстов, 401 без токена, `/health`, `/swagger/doc.json`;
- middleware: access-лог пишется для обычного запроса и для запроса с паникой
  (в последнем случае — со статусом 500), паника не роняет процесс.

Запуск:

```bash
go test ./...            # все тесты
go test ./... -v         # с именами тестов
go test ./tests -run TestAPIFlow -v
```

Интеграционных тестов с настоящей БД нет: в текущем окружении нет ни Docker,
ни PostgreSQL. Их логичное место — `tests/integration_test.go` под тегом
`//go:build integration` с `TEST_DATABASE_URL`.

---

## 11. Как проверить руками

```bash
# 1. поднять Postgres и Kafka
docker compose up -d postgres kafka

# 2. запустить сервис
./main -c config.env

# 3. регистрация
curl -X POST localhost:8080/api/v1/register -H 'Content-Type: application/json' \
  -d '{"username":"alice","password":"secret123","email":"alice@example.com"}'

# 4. логин → токен
curl -X POST localhost:8080/api/v1/login -H 'Content-Type: application/json' \
  -d '{"username":"alice","password":"secret123"}'

# 5. дальше с заголовком Authorization: Bearer <токен>
curl -X POST localhost:8080/api/v1/wallet/deposit -H "Authorization: Bearer $T" \
  -H 'Content-Type: application/json' -d '{"amount":100,"currency":"USD"}'
```

Спека: `http://localhost:8080/swagger/doc.json` (JSON) и `/swagger/` (страница).

Что смотреть в БД после прогона:

```sql
SELECT username, email FROM users;
SELECT currency, amount FROM balances;
SELECT operation, status, amount, result_amount, rate, rub_equivalent FROM transactions ORDER BY created_at DESC;
```

---

## 12. Связь с остальными сервисами

| Сервис | Роль в общей схеме | Что уже есть здесь |
|---|---|---|
| `exchange` (gw-exchanger) | единственный источник курсов | gRPC-клиент и контракт `proto-exchange` |
| `notification` | сохраняет историю крупных переводов (Kafka → MongoDB) | события в `wallet.transactions` с нужными полями |
| `analytics` | считает метрики (Kafka → ClickHouse) | `status` в событиях (для частоты ошибок), `created_at` (для latency) |

То есть кошелёк уже отдаёт всё, что нужно потребителям: идентификаторы, статус,
суммы, курс, рублёвый эквивалент и время создания.

---

## 13. Глоссарий

| Термин | Простыми словами |
|---|---|
| **JWT** | подписанный токен: сервер выдаёт строку, клиент носит её с собой, сервер проверяет подпись и не хранит сессии |
| **bcrypt** | медленный хеш для паролей: подобрать по украденному хешу дорого |
| **gRPC** | вызов функций другого сервиса так, будто это локальные функции; контракт описан в `.proto` |
| **миграция** | SQL-файл, который меняет схему БД; применяется один раз, версия фиксируется в `schema_migrations` |
| **TTL-кэш** | память с «сроком годности»: пока данные свежие — отдаём их, потом идём за новыми |
| **graceful shutdown** | аккуратная остановка: дождаться текущих запросов, закрыть соединения, ничего не потерять |
| **N+1 / FOR UPDATE** | блокировка строки БД на время транзакции — так параллельные запросы не портят баланс |
| **At-least-once** | гарантия доставки «минимум один раз»: дубли возможны, поэтому потребитель должен быть идемпотентным |
| **идемпотентность** | повторная обработка того же сообщения не меняет результат |
| **DTO** | структура, описывающая JSON запроса/ответа |
| **sentinel-ошибка** | заранее объявленная ошибка (`ErrInsufficientFunds`), которую сравнивают через `errors.Is` |
