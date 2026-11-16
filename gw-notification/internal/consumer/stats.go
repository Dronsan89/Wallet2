package consumer

import "sync/atomic"

// Stats — счётчики обработки сообщений.
type Stats struct {
	Received   atomic.Int64
	Stored     atomic.Int64
	Duplicates atomic.Int64
	Skipped    atomic.Int64
	Poison     atomic.Int64
	Retries    atomic.Int64
	DLQ        atomic.Int64
	Committed  atomic.Int64
}

// Snapshot — снимок счётчиков для логов и ручки /stats.
type Snapshot struct {
	Received   int64 `json:"received"`
	Stored     int64 `json:"stored"`
	Duplicates int64 `json:"duplicates"`
	Skipped    int64 `json:"skipped"`
	Poison     int64 `json:"poison"`
	Retries    int64 `json:"retries"`
	DLQ        int64 `json:"dlq"`
	Committed  int64 `json:"committed"`
}

// Snapshot снимает текущие значения счётчиков.
func (s *Stats) Snapshot() Snapshot {
	return Snapshot{
		Received:   s.Received.Load(),
		Stored:     s.Stored.Load(),
		Duplicates: s.Duplicates.Load(),
		Skipped:    s.Skipped.Load(),
		Poison:     s.Poison.Load(),
		Retries:    s.Retries.Load(),
		DLQ:        s.DLQ.Load(),
		Committed:  s.Committed.Load(),
	}
}