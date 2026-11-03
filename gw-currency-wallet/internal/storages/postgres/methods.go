package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/shopspring/decimal"

	"github.com/Dronsan89/Wallet2/gw-currency-wallet/internal/storages"
	"github.com/Dronsan89/Wallet2/gw-currency-wallet/pkg"
)

// CreateUser создаёт пользователя и нулевые балансы по всем валютам.
func (c *Connector) CreateUser(ctx context.Context, username, email, passwordHash string) (storages.User, error) {
	var user storages.User

	tx, err := c.pool.Begin(ctx)
	if err != nil {
		return user, fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	const insertUser = `
		INSERT INTO users (username, email, password_hash)
		VALUES ($1, $2, $3)
		RETURNING id, username, email, password_hash, created_at`

	err = tx.QueryRow(ctx, insertUser, username, email, passwordHash).
		Scan(&user.ID, &user.Username, &user.Email, &user.PasswordHash, &user.CreatedAt)
	if err != nil {
		if isUniqueViolation(err) {
			return storages.User{}, storages.ErrUserExists
		}
		return storages.User{}, fmt.Errorf("insert user: %w", err)
	}

	const insertBalance = `INSERT INTO balances (user_id, currency, amount) VALUES ($1, $2, 0)`
	for _, currency := range pkg.SupportedCurrencies {
		if _, err := tx.Exec(ctx, insertBalance, user.ID, currency); err != nil {
			return storages.User{}, fmt.Errorf("init balance %s: %w", currency, err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return storages.User{}, fmt.Errorf("commit create user: %w", err)
	}

	return user, nil
}

// UserByUsername возвращает пользователя по имени.
func (c *Connector) UserByUsername(ctx context.Context, username string) (storages.User, error) {
	const q = `SELECT id, username, email, password_hash, created_at FROM users WHERE username = $1`

	var user storages.User
	err := c.pool.QueryRow(ctx, q, username).
		Scan(&user.ID, &user.Username, &user.Email, &user.PasswordHash, &user.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return storages.User{}, storages.ErrUserNotFound
		}
		return storages.User{}, fmt.Errorf("select user: %w", err)
	}

	return user, nil
}

// Balances возвращает баланс по всем поддерживаемым валютам.
func (c *Connector) Balances(ctx context.Context, userID uuid.UUID) (storages.Balances, error) {
	const q = `SELECT currency, amount::text FROM balances WHERE user_id = $1`

	rows, err := c.pool.Query(ctx, q, userID)
	if err != nil {
		return nil, fmt.Errorf("select balances: %w", err)
	}
	defer rows.Close()

	balances := make(storages.Balances, len(pkg.SupportedCurrencies))
	for _, currency := range pkg.SupportedCurrencies {
		balances[currency] = decimal.Zero
	}

	for rows.Next() {
		var (
			currency string
			amount   string
		)
		if err := rows.Scan(&currency, &amount); err != nil {
			return nil, fmt.Errorf("scan balance: %w", err)
		}

		value, err := decimal.NewFromString(amount)
		if err != nil {
			return nil, fmt.Errorf("parse balance %s: %w", currency, err)
		}
		balances[currency] = value
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate balances: %w", err)
	}

	return balances, nil
}

// Deposit пополняет баланс и записывает транзакцию.
func (c *Connector) Deposit(ctx context.Context, t storages.Transaction) (storages.Balances, error) {
	tx, err := c.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	const q = `
		UPDATE balances
		   SET amount = amount + $1::numeric, updated_at = now()
		 WHERE user_id = $2 AND currency = $3`

	if _, err := tx.Exec(ctx, q, t.Amount.String(), t.UserID, t.FromCurrency); err != nil {
		return nil, fmt.Errorf("deposit: %w", err)
	}

	if err := insertTransaction(ctx, tx, t); err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit deposit: %w", err)
	}

	return c.Balances(ctx, t.UserID)
}

// Withdraw списывает средства и записывает транзакцию.
func (c *Connector) Withdraw(ctx context.Context, t storages.Transaction) (storages.Balances, error) {
	tx, err := c.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	const q = `
		UPDATE balances
		   SET amount = amount - $1::numeric, updated_at = now()
		 WHERE user_id = $2 AND currency = $3 AND amount >= $1::numeric`

	tag, err := tx.Exec(ctx, q, t.Amount.String(), t.UserID, t.FromCurrency)
	if err != nil {
		return nil, fmt.Errorf("withdraw: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return nil, storages.ErrInsufficientFunds
	}

	if err := insertTransaction(ctx, tx, t); err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit withdraw: %w", err)
	}

	return c.Balances(ctx, t.UserID)
}

// Exchange обменивает валюту: блокирует обе строки баланса в детерминированном
// порядке, проверяет средства и обновляет суммы в одной транзакции.
func (c *Connector) Exchange(ctx context.Context, t storages.Transaction) (storages.Balances, error) {
	tx, err := c.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	const lock = `
		SELECT currency FROM balances
		 WHERE user_id = $1 AND currency = ANY($2)
		 ORDER BY currency
		 FOR UPDATE`

	rows, err := tx.Query(ctx, lock, t.UserID, []string{t.FromCurrency, t.ToCurrency})
	if err != nil {
		return nil, fmt.Errorf("lock balances: %w", err)
	}
	locked := 0
	for rows.Next() {
		locked++
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("lock balances: %w", err)
	}
	if locked < 2 {
		return nil, storages.ErrInsufficientFunds
	}

	const withdrawQ = `
		UPDATE balances
		   SET amount = amount - $1::numeric, updated_at = now()
		 WHERE user_id = $2 AND currency = $3 AND amount >= $1::numeric`

	tag, err := tx.Exec(ctx, withdrawQ, t.Amount.String(), t.UserID, t.FromCurrency)
	if err != nil {
		return nil, fmt.Errorf("exchange withdraw: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return nil, storages.ErrInsufficientFunds
	}

	const depositQ = `
		UPDATE balances
		   SET amount = amount + $1::numeric, updated_at = now()
		 WHERE user_id = $2 AND currency = $3`

	if _, err := tx.Exec(ctx, depositQ, t.ResultAmount.String(), t.UserID, t.ToCurrency); err != nil {
		return nil, fmt.Errorf("exchange deposit: %w", err)
	}

	if err := insertTransaction(ctx, tx, t); err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit exchange: %w", err)
	}

	return c.Balances(ctx, t.UserID)
}

// insertTransaction записывает операцию в историю.
func insertTransaction(ctx context.Context, tx pgx.Tx, t storages.Transaction) error {
	const q = `
		INSERT INTO transactions
			(id, user_id, operation, status, from_currency, to_currency, amount, result_amount, rate, rub_equivalent, created_at)
		VALUES
			($1, $2, $3, $4, $5, $6, $7::numeric, $8::numeric, $9::numeric, $10::numeric, $11)`

	_, err := tx.Exec(ctx, q,
		t.ID,
		t.UserID,
		t.Operation,
		t.Status,
		nullableString(t.FromCurrency),
		nullableString(t.ToCurrency),
		t.Amount.String(),
		t.ResultAmount.String(),
		t.Rate.String(),
		t.RubEquivalent.String(),
		t.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("insert transaction: %w", err)
	}

	return nil
}

func nullableString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}