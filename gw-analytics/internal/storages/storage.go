// Package storages описывает доступ к аналитическому хранилищу.
// Интерфейс изолирует консьюмер и HTTP-слой от конкретной СУБД.
package storages

import (
	"context"
	"time"

	"github.com/Dronsan89/Wallet2/gw-analytics/internal/model"
)

// Storage — хранилище событий и агрегатов.
type Storage interface {
	// InsertBatch сохраняет пакет событий; возвращает число вставленных строк.
	InsertBatch(ctx context.Context, events []model.Event) (int, error)
	// Summary считает сводные метрики за окно [from, to).
	Summary(ctx context.Context, from, to time.Time) (model.Summary, error)
	// TimeSeries считает метрики в бакетах шагом step за окно [from, to).
	TimeSeries(ctx context.Context, from, to time.Time, step time.Duration) ([]model.SeriesPoint, error)
	// Ping проверяет доступность хранилища для пробы готовности.
	Ping(ctx context.Context) error
}