package consumer

import "sync/atomic"

// Stats — счётчики обработки сообщений.
type Stats struct {
	Received  atomic.Int64
	Stored    atomic.Int64
	Poison    atomic.Int64
	Retries   atomic.Int64
	Dropped   atomic.Int64
	Committed atomic.Int64
}

// Snapshot — снимок счётчиков для логов и ручки /stats.
type Snapshot struct {
	Received  int64 `json:"received"`
	Stored    int64 `json:"stored"`
	Poison    int64 `json:"poison"`
	Retries   int64 `json:"retries"`
	Dropped   int64 `json:"dropped"`
	Committed int64 `json:"committed"`
}

// Snapshot снимает текущие значения счётчиков.
func (s *Stats) Snapshot() Snapshot {
	return Snapshot{
		Received:  s.Received.Load(),
		Stored:    s.Stored.Load(),
		Poison:    s.Poison.Load(),
		Retries:   s.Retries.Load(),
		Dropped:   s.Dropped.Load(),
		Committed: s.Committed.Load(),
	}
}