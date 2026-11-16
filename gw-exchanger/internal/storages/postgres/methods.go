package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/Dronsan89/Wallet2/gw-exchanger/internal/storages"
)

// Rates возвращает курсы всех валют из таблицы currency_rates.
func (c *Connector) Rates(ctx context.Context) (storages.Rates, error) {
	const q = `SELECT currency, rub_per_unit::text FROM currency_rates`

	rows, err := c.pool.Query(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("select currency rates: %w", err)
	}
	defer rows.Close()

	rates := make(storages.Rates, 3)

	for rows.Next() {
		var (
			currency string
			raw      string
		)
		if err := rows.Scan(&currency, &raw); err != nil {
			return nil, fmt.Errorf("scan currency rate: %w", err)
		}

		value, err := decimal.NewFromString(raw)
		if err != nil {
			return nil, fmt.Errorf("parse rate for %s: %w", currency, err)
		}

		rates[currency] = value
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate currency rates: %w", err)
	}

	return rates, nil
}

// Rate возвращает курс одной валюты к рублю.
func (c *Connector) Rate(ctx context.Context, currency string) (decimal.Decimal, error) {
	const q = `SELECT rub_per_unit::text FROM currency_rates WHERE currency = $1`

	var raw string
	if err := c.pool.QueryRow(ctx, q, currency).Scan(&raw); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return decimal.Zero, storages.ErrCurrencyNotFound
		}
		return decimal.Zero, fmt.Errorf("select rate for %s: %w", currency, err)
	}

	value, err := decimal.NewFromString(raw)
	if err != nil {
		return decimal.Zero, fmt.Errorf("parse rate for %s: %w", currency, err)
	}

	return value, nil
}