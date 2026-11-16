package httpserver

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Dronsan89/Wallet2/gw-analytics/internal/consumer"
	"github.com/Dronsan89/Wallet2/gw-analytics/internal/model"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// fakeStorage отдаёт заданные агрегаты и запоминает параметры запроса.
type fakeStorage struct {
	summary  model.Summary
	points   []model.SeriesPoint
	err      error
	pingErr  error
	lastFrom time.Time
	lastTo   time.Time
	lastStep time.Duration
}

func (f *fakeStorage) InsertBatch(context.Context, []model.Event) (int, error) { return 0, nil }

func (f *fakeStorage) Summary(_ context.Context, from, to time.Time) (model.Summary, error) {
	f.lastFrom, f.lastTo = from, to
	if f.err != nil {
		return model.Summary{}, f.err
	}

	return f.summary, nil
}

func (f *fakeStorage) TimeSeries(_ context.Context, from, to time.Time, step time.Duration) ([]model.SeriesPoint, error) {
	f.lastFrom, f.lastTo, f.lastStep = from, to, step
	if f.err != nil {
		return nil, f.err
	}

	return f.points, nil
}

func (f *fakeStorage) Ping(context.Context) error { return f.pingErr }

// stubStats отдаёт фиксированный снимок счётчиков.
type stubStats struct{ snap consumer.Snapshot }

func (s stubStats) Stats() consumer.Snapshot { return s.snap }

func newServer(storage *fakeStorage) (*Server, *httptest.Server) {
	srv := New(":0", storage, stubStats{snap: consumer.Snapshot{Received: 5, Stored: 5}}, testLogger())
	srv.now = func() time.Time { return time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC) }

	return srv, httptest.NewServer(srv.srv.Handler)
}

func TestSummary(t *testing.T) {
	storage := &fakeStorage{summary: model.Summary{
		Total:       10,
		Failed:      2,
		ErrorRate:   0.2,
		ByOperation: map[string]int64{"DEPOSIT": 8, "EXCHANGE": 2},
		ByStatus:    map[string]int64{"SUCCESS": 8, "FAILED": 2},
		Latency:     model.LatencyStats{Avg: 1.5, P95: 3, Max: 9},
	}}
	_, ts := newServer(storage)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/v1/analytics/summary?period=5m")
	if err != nil {
		t.Fatalf("GET summary: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var got model.Summary
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Total != 10 || got.Failed != 2 || got.Period != "5m" {
		t.Fatalf("summary = %+v", got)
	}
	if got.Latency.P95 != 3 {
		t.Fatalf("p95 = %v, want 3", got.Latency.P95)
	}

	// Окно должно быть [now-5m, now).
	if want := storage.lastTo.Add(-5 * time.Minute); !storage.lastFrom.Equal(want) {
		t.Fatalf("from = %v, want %v", storage.lastFrom, want)
	}
}

func TestSummaryDefaultPeriod(t *testing.T) {
	storage := &fakeStorage{}
	_, ts := newServer(storage)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/v1/analytics/summary")
	if err != nil {
		t.Fatalf("GET summary: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var got model.Summary
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Period != "5m" {
		t.Fatalf("period = %q, want 5m по умолчанию", got.Period)
	}
}

func TestSummaryInvalidPeriod(t *testing.T) {
	storage := &fakeStorage{}
	_, ts := newServer(storage)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/v1/analytics/summary?period=42")
	if err != nil {
		t.Fatalf("GET summary: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

func TestSummaryStorageError(t *testing.T) {
	storage := &fakeStorage{err: errors.New("clickhouse is down")}
	_, ts := newServer(storage)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/v1/analytics/summary?period=1h")
	if err != nil {
		t.Fatalf("GET summary: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", resp.StatusCode)
	}
}

func TestTimeSeries(t *testing.T) {
	storage := &fakeStorage{points: []model.SeriesPoint{
		{Bucket: time.Date(2026, 11, 14, 11, 59, 0, 0, time.UTC), Total: 4, Failed: 1, LatencyAvgMS: 2.5},
		{Bucket: time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC), Total: 6, Failed: 0, LatencyAvgMS: 1.5},
	}}
	_, ts := newServer(storage)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/v1/analytics/timeseries?period=5m&step=30s")
	if err != nil {
		t.Fatalf("GET timeseries: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var got model.TimeSeries
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got.Points) != 2 {
		t.Fatalf("точек = %d, want 2", len(got.Points))
	}
	if got.Step != "30s" || got.Period != "5m" {
		t.Fatalf("period/step = %q/%q", got.Period, got.Step)
	}
	if storage.lastStep != 30*time.Second {
		t.Fatalf("шаг запроса = %v, want 30s", storage.lastStep)
	}
}

func TestTimeSeriesInvalidStep(t *testing.T) {
	storage := &fakeStorage{}
	_, ts := newServer(storage)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/v1/analytics/timeseries?period=1m&step=1h")
	if err != nil {
		t.Fatalf("GET timeseries: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (шаг больше периода)", resp.StatusCode)
	}
}

func TestHealthReadyStats(t *testing.T) {
	storage := &fakeStorage{}
	_, ts := newServer(storage)
	defer ts.Close()

	for _, path := range []string{"/health", "/ready", "/stats"} {
		resp, err := http.Get(ts.URL + path)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("%s: status = %d, want 200", path, resp.StatusCode)
		}
	}

	storage.pingErr = errors.New("clickhouse is down")

	resp, err := http.Get(ts.URL + "/ready")
	if err != nil {
		t.Fatalf("GET /ready (broken): %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("/ready: status = %d, want 503", resp.StatusCode)
	}
}