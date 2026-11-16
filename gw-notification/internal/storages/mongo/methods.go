package mongo

import (
	"context"
	"errors"
	"fmt"

	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"github.com/Dronsan89/Wallet2/gw-notification/internal/model"
	"github.com/Dronsan89/Wallet2/gw-notification/internal/storages"
)

// duplicateKeyCode — код ошибки MongoDB «нарушен уникальный индекс».
const duplicateKeyCode = 11000

// InsertMany сохраняет пакет событий. Вставка неупорядоченная (ordered=false):
// одно «плохое» событие не блокирует остальные. Дубликаты по transaction_id
// считаются отдельно и не считаются ошибкой — это и есть идемпотентность.
func (c *Connector) InsertMany(ctx context.Context, events []model.Event) (storages.InsertResult, error) {
	var result storages.InsertResult

	if len(events) == 0 {
		return result, nil
	}

	documents := make([]any, len(events))
	for i := range events {
		documents[i] = events[i]
	}

	_, err := c.collection.InsertMany(ctx, documents, options.InsertMany().SetOrdered(false))
	if err == nil {
		result.Inserted = int64(len(events))
		return result, nil
	}

	duplicates, err := countDuplicates(err)
	if err != nil {
		return result, err
	}

	result.Duplicates = duplicates
	result.Inserted = int64(len(events)) - duplicates

	return result, nil
}

// countDuplicates считает ошибки «дубликат по уникальному индексу».
// Пакетная вставка возвращает BulkWriteException, одиночные операции —
// WriteException, поэтому поддерживаем оба случая. Любая другая ошибка
// возвращается наружу: такой батч должен уйти в DLQ.
func countDuplicates(err error) (int64, error) {
	var duplicates int64

	var bulkErr mongo.BulkWriteException
	if errors.As(err, &bulkErr) {
		for _, writeError := range bulkErr.WriteErrors {
			if writeError.Code == duplicateKeyCode {
				duplicates++
				continue
			}

			return duplicates, fmt.Errorf("insert event: %s", writeError.Message)
		}

		return duplicates, nil
	}

	var writeErr mongo.WriteException
	if errors.As(err, &writeErr) {
		for _, writeError := range writeErr.WriteErrors {
			if writeError.Code == duplicateKeyCode {
				duplicates++
				continue
			}

			return duplicates, fmt.Errorf("insert event: %s", writeError.Message)
		}

		return duplicates, nil
	}

	return 0, fmt.Errorf("insert events: %w", err)
}
