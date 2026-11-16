-- Таблица событий аналитики.
-- {database} подставляется сервисом при старте (имя базы из CLICKHOUSE_DB).
--
-- ReplacingMergeTree: дубли по event_id схлопываются при merge, поэтому запросы
-- используют FINAL. Партиционирование по месяцу события ускоряет окна 1d/1w.

CREATE TABLE IF NOT EXISTS {database}.events
(
    event_id       String,
    transaction_id String,
    user_id        String,
    operation      LowCardinality(String),
    status         LowCardinality(String),
    from_currency  LowCardinality(String),
    to_currency    LowCardinality(String),
    amount         Float64,
    rate           Float64,
    result_amount  Float64,
    rub_equivalent Float64,
    created_at     DateTime64(3, 'UTC'),
    received_at    DateTime64(3, 'UTC'),
    latency_ms     Int64,
    source         LowCardinality(String),
    schema_version Int32
)
ENGINE = ReplacingMergeTree(received_at)
PARTITION BY toYYYYMM(created_at)
ORDER BY event_id;