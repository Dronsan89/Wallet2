package clickhouse

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/Dronsan89/Wallet2/gw-analytics/internal/model"
)

// InsertBatch вставляет пакет событий одним batch-запросом.
func (c *Connector) InsertBatch(ctx context.Context, events []model.Event) (int, error) {
	if len(events) == 0 {
		return 0, nil
	}

	batch, err := c.conn.PrepareBatch(ctx, "INSERT INTO "+c.table())
	if err != nil {
		return 0, fmt.Errorf("prepare batch: %w", err)
	}

	for _, event := range events {
		err := batch.Append(
			event.EventID,
			event.TransactionID,
			event.UserID,
			event.Operation,
			event.Status,
			event.FromCurrency,
			event.ToCurrency,
			event.Amount,
			event.Rate,
			event.ResultAmount,
			event.RubEquivalent,
			event.CreatedAt,
			event.ReceivedAt,
			event.LatencyMS,
			event.Source,
			int32(event.SchemaVersion),
		)
		if err != nil {
			return 0, fmt.Errorf("append event %s: %w", event.EventID, err)
		}
	}

	if err := batch.Send(); err != nil {
		return 0, fmt.Errorf("send batch: %w", err)
	}

	return len(events), nil
}

// Summary считает сводные метрики за окно [from, to).
// FINAL нужен, потому что ReplacingMergeTree схлопывает дубли только при merge.
func (c *Connector) Summary(ctx context.Context, from, to time.Time) (model.Summary, error) {
	summary := model.Summary{
		From:        from,
		To:          to,
		ByOperation: make(map[string]int64, 3),
		ByStatus:    make(map[string]int64, 2),
	}

	const totalsQuery = `
		SELECT
			count()                        AS total,
			countIf(status = ?)            AS failed,
			avg(latency_ms)                AS avg_latency,
			quantile(0.95)(latency_ms)     AS p95_latency,
			max(latency_ms)                AS max_latency
		FROM %s FINAL
		WHERE created_at >= ? AND created_at < ?`

	var (
		total      uint64
		failed     uint64
		avgLatency float64
		p95Latency float64
		maxLatency float64
	)

	row := c.conn.QueryRow(ctx, fmt.Sprintf(totalsQuery, c.table()), model.StatusFailed, from, to)
	if err := row.Scan(&total, &failed, &avgLatency, &p95Latency, &maxLatency); err != nil {
		return summary, fmt.Errorf("summary totals: %w", err)
	}

	summary.Total = int64(total)
	summary.Failed = int64(failed)

	if total > 0 {
		summary.ErrorRate = float64(failed) / float64(total)
		summary.Latency = model.LatencyStats{
			Avg: roundMetric(avgLatency),
			P95: roundMetric(p95Latency),
			Max: roundMetric(maxLatency),
		}
	}

	byOperation, err := c.countBy(ctx, "operation", from, to)
	if err != nil {
		return summary, err
	}
	summary.ByOperation = byOperation

	byStatus, err := c.countBy(ctx, "status", from, to)
	if err != nil {
		return summary, err
	}
	summary.ByStatus = byStatus

	return summary, nil
}

// countBy группирует события по указанной колонке (operation или status).
func (c *Connector) countBy(ctx context.Context, column string, from, to time.Time) (map[string]int64, error) {
	// column — не пользовательский ввод: значения только из кода вызывающего.
	const query = `
		SELECT %s AS key, count() AS total
		FROM %s FINAL
		WHERE created_at >= ? AND created_at < ?
		GROUP BY key`

	rows, err := c.conn.Query(ctx, fmt.Sprintf(query, quoteIdent(column), c.table()), from, to)
	if err != nil {
		return nil, fmt.Errorf("group by %s: %w", column, err)
	}
	defer rows.Close()

	result := make(map[string]int64, 4)

	for rows.Next() {
		var (
			key   string
			total uint64
		)
		if err := rows.Scan(&key, &total); err != nil {
			return nil, fmt.Errorf("scan group by %s: %w", column, err)
		}
		result[key] = int64(total)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate group by %s: %w", column, err)
	}

	return result, nil
}

// TimeSeries считает метрики в бакетах шагом step.
func (c *Connector) TimeSeries(ctx context.Context, from, to time.Time, step time.Duration) ([]model.SeriesPoint, error) {
	seconds := int64(step.Seconds())
	if seconds < 1 {
		seconds = 1
	}

	const query = `
		SELECT
			toStartOfInterval(created_at, toIntervalSecond(?)) AS bucket,
			count()                AS total,
			countIf(status = ?)    AS failed,
			avg(latency_ms)        AS avg_latency
		FROM %s FINAL
		WHERE created_at >= ? AND created_at < ?
		GROUP BY bucket
		ORDER BY bucket`

	rows, err := c.conn.Query(ctx, fmt.Sprintf(query, c.table()), seconds, model.StatusFailed, from, to)
	if err != nil {
		return nil, fmt.Errorf("timeseries: %w", err)
	}
	defer rows.Close()

	points := make([]model.SeriesPoint, 0, 64)

	for rows.Next() {
		var (
			point   model.SeriesPoint
			total   uint64
			failed  uint64
			avgLat  float64
		)
		if err := rows.Scan(&point.Bucket, &total, &failed, &avgLat); err != nil {
			return nil, fmt.Errorf("scan timeseries: %w", err)
		}

		point.Total = int64(total)
		point.Failed = int64(failed)
		point.LatencyAvgMS = roundMetric(avgLat)

		points = append(points, point)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate timeseries: %w", err)
	}

	return points, nil
}

// roundMetric округляет значение до сотых и убирает NaN/Inf от ClickHouse.
func roundMetric(value float64) float64 {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return 0
	}

	return math.Round(value*100) / 100
}