package app

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"
)

type fakeApp struct {
	startErr     error
	stopErr      error
	started      bool
	stopped      bool
	startTimeout time.Duration
	stopTimeout  time.Duration
}

func (f *fakeApp) Start(context.Context) error { f.started = true; return f.startErr }
func (f *fakeApp) Stop(context.Context) error  { f.stopped = true; return f.stopErr }
func (f *fakeApp) StartTimeout() time.Duration { return f.startTimeout }
func (f *fakeApp) StopTimeout() time.Duration  { return f.stopTimeout }

func testLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func TestRunWithSignalsStopsOnSignal(t *testing.T) {
	quit := make(chan os.Signal, 1)
	quit <- os.Interrupt

	a := &fakeApp{startTimeout: time.Second, stopTimeout: time.Second}
	if err := runWithSignals(a, testLogger(), quit); err != nil {
		t.Fatalf("runWithSignals() error = %v, want nil", err)
	}

	if !a.started {
		t.Error("Start was not called")
	}
	if !a.stopped {
		t.Error("Stop was not called after shutdown signal")
	}
}

func TestRunWithSignalsStartError(t *testing.T) {
	want := errors.New("start failed")
	a := &fakeApp{startErr: want, startTimeout: time.Second, stopTimeout: time.Second}

	err := runWithSignals(a, testLogger(), make(chan os.Signal))
	if !errors.Is(err, want) {
		t.Fatalf("runWithSignals() error = %v, want %v", err, want)
	}
	if a.stopped {
		t.Error("Stop must not be called when Start failed")
	}
}

func TestRunWithSignalsStopError(t *testing.T) {
	want := errors.New("stop failed")
	quit := make(chan os.Signal, 1)
	quit <- os.Interrupt

	a := &fakeApp{stopErr: want, startTimeout: time.Second, stopTimeout: time.Second}

	err := runWithSignals(a, testLogger(), quit)
	if !errors.Is(err, want) {
		t.Fatalf("runWithSignals() error = %v, want %v", err, want)
	}
}

func TestRunNilApplication(t *testing.T) {
	if err := Run(nil, testLogger()); err == nil {
		t.Fatal("Run(nil) must return an error")
	}
}