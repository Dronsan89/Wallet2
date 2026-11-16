// Package storages описывает доступ к хранилищу истории переводов.
// Интерфейс изолирует консьюмер от конкретной БД.
package storages

import (
	"context"

	"github.com/Dronsan89/Wallet2/gw-notification/internal/model"
)

// InsertResult — итог пакетного сохранения событий.
type InsertResult struct {
	Inserted   int64
	Duplicates int64
}

// Storage — хранилище истории крупных переводов.
type Storage interface {
	// InsertMany сохраняет пакет событий. Повторное событие с тем же
	// transaction_id не создаёт второй документ (идемпотентность).
	InsertMany(ctx context.Context, events []model.Event) (InsertResult, error)
	// Ping проверяет доступность хранилища для проб готовности.
	Ping(ctx context.Context) error
}