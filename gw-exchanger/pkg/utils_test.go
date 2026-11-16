package pkg

import (
	"reflect"
	"testing"
	"time"
)

func TestGetEnv(t *testing.T) {
	const key = "WALLET2_TEST_STRING"

	t.Setenv(key, "value")
	if got := GetEnv(key, "fallback"); got != "value" {
		t.Errorf("GetEnv() = %q, want %q", got, "value")
	}

	t.Setenv(key, "")
	if got := GetEnv(key, "fallback"); got != "fallback" {
		t.Errorf("GetEnv() for empty variable = %q, want %q", got, "fallback")
	}
}

func TestGetEnvInt(t *testing.T) {
	const key = "WALLET2_TEST_INT"

	t.Setenv(key, "42")
	if got := GetEnvInt(key, 7); got != 42 {
		t.Errorf("GetEnvInt() = %d, want 42", got)
	}

	t.Setenv(key, "not-a-number")
	if got := GetEnvInt(key, 7); got != 7 {
		t.Errorf("GetEnvInt() for invalid value = %d, want fallback 7", got)
	}
}

func TestGetEnvBool(t *testing.T) {
	const key = "WALLET2_TEST_BOOL"

	t.Setenv(key, "true")
	if got := GetEnvBool(key, false); !got {
		t.Error("GetEnvBool() = false, want true")
	}

	t.Setenv(key, "nonsense")
	if got := GetEnvBool(key, true); !got {
		t.Error("GetEnvBool() for invalid value must return fallback true")
	}
}

func TestGetEnvDuration(t *testing.T) {
	const key = "WALLET2_TEST_DURATION"

	t.Setenv(key, "5s")
	if got := GetEnvDuration(key, time.Second); got != 5*time.Second {
		t.Errorf("GetEnvDuration() = %v, want 5s", got)
	}

	t.Setenv(key, "nonsense")
	if got := GetEnvDuration(key, 2*time.Second); got != 2*time.Second {
		t.Errorf("GetEnvDuration() for invalid value = %v, want fallback 2s", got)
	}
}

func TestGetEnvList(t *testing.T) {
	const key = "WALLET2_TEST_LIST"

	t.Setenv(key, "localhost:9092, localhost:9093 ,")
	want := []string{"localhost:9092", "localhost:9093"}
	if got := GetEnvList(key, nil); !reflect.DeepEqual(got, want) {
		t.Errorf("GetEnvList() = %v, want %v", got, want)
	}

	t.Setenv(key, "")
	fallback := []string{"localhost:9092"}
	if got := GetEnvList(key, fallback); !reflect.DeepEqual(got, fallback) {
		t.Errorf("GetEnvList() for empty variable = %v, want %v", got, fallback)
	}
}