-- Курсы валют сервиса exchanger: сколько рублей стоит одна единица валюты.

CREATE TABLE IF NOT EXISTS currency_rates (
    currency     CHAR(3) PRIMARY KEY CHECK (currency IN ('USD', 'RUB', 'EUR')),
    rub_per_unit NUMERIC(18, 6) NOT NULL CHECK (rub_per_unit > 0),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Стартовые курсы из ТЗ. ON CONFLICT DO NOTHING позволяет безопасно
-- перезапускать миграцию и не затирать курсы, изменённые вручную.
INSERT INTO currency_rates (currency, rub_per_unit) VALUES
    ('USD', 90.000000),
    ('EUR', 100.000000),
    ('RUB', 1.000000)
ON CONFLICT (currency) DO NOTHING;
