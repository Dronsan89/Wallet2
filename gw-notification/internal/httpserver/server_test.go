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

	"github.com/Dronsan89/Wallet2/gw-notification/internal/consumer"
	"github.com/Dronsan89/Wallet2/gw-notification/internal/model"
	"github.com/Dronsan89/Wallet2/gw-notification/internal/storages"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// stubStorage управляет результатом Ping.
type stubStorage struct{ pingErr error }

func (s stubStorage) InsertMany(context.Context, []model.Event) (storages.InsertResult, error) {
	return storages.InsertResult{}, nil
}

func (s stubStorage) Ping(context.Context) error { return s.pingErr }

// stubStats отдаёт фиксированный снимок счётчиков.
type stubStats struct{ snap consumer.Snapshot }

func (s stubStats) Stats() consumer.Snapshot { return s.snap }

func newTestServer(storage storages.Storage, stats consumer.Snapshot) *httptest.Server {
	srv := New(":0", storage, stubStats{snap: stats}, testLogger())

	return httptest.NewServer(srv.srv.Handler)
}

func TestHealth(t *testing.T) {
	ts := newTestServer(stubStorage{}, consumer.Snapshot{})
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/health")
	if err != nil {
		t.Fatalf("GET /health: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
}

func TestReady(t *testing.T) {
	ts := newTestServer(stubStorage{}, consumer.Snapshot{})
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/ready")
	if err != nil {
		t.Fatalf("GET /ready: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	broken := newTestServer(stubStorage{pingErr: errors.New("mongo is down")}, consumer.Snapshot{})
	defer broken.Close()

	respBroken, err := http.Get(broken.URL + "/ready")
	if err != nil {
		t.Fatalf("GET /ready (broken): %v", err)
	}
	defer respBroken.Body.Close()

	if respBroken.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", respBroken.StatusCode)
	}
}

func TestStats(t *testing.T) {
	ts := newTestServer(stubStorage{}, consumer.Snapshot{Received: 10, Stored: 7, Duplicates: 2, Skipped: 1})
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/stats")
	if err != nil {
		t.Fatalf("GET /stats: %v", err)
	}
	defer resp.Body.Close()

	var got consumer.Snapshot
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode /stats: %v", err)
	}

	if got.Received != 10 || got.Stored != 7 || got.Duplicates != 2 || got.Skipped != 1 {
		t.Fatalf("stats = %+v", got)
	}
}