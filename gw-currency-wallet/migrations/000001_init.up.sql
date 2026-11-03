-- Схема gw-currency-wallet: пользователи, балансы по валютам, история операций.
-- gen_random_uuid() входит в ядро PostgreSQL начиная с 13 версии, расширение не нужно.

CREATE TABLE IF NOT EXISTS users (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    username      TEXT NOT NULL UNIQUE,
    email         TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS balances (
    user_id    UUID NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    currency   CHAR(3) NOT NULL CHECK (currency IN ('USD', 'RUB', 'EUR')),
    amount     NUMERIC(20, 2) NOT NULL DEFAULT 0 CHECK (amount >= 0),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, currency)
);

CREATE TABLE IF NOT EXISTS transactions (
    id             UUID PRIMARY KEY,
    user_id        UUID NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    operation      TEXT NOT NULL CHECK (operation IN ('DEPOSIT', 'WITHDRAW', 'EXCHANGE')),
    status         TEXT NOT NULL CHECK (status IN ('SUCCESS', 'FAILED')),
    from_currency  CHAR(3),
    to_currency    CHAR(3),
    amount         NUMERIC(20, 2) NOT NULL,
    result_amount  NUMERIC(20, 2),
    rate           NUMERIC(20, 8),
    rub_equivalent NUMERIC(20, 2),
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS transactions_user_created_idx
    ON transactions (user_id, created_at DESC);
