package logger_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	logging "github.com/SomeBlackMagic/go-logger"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func ptr[T any](v T) *T { return &v }
func newLogger(t *testing.T, c logging.Config) (*logging.Logger, *bytes.Buffer) {
	t.Helper()
	t.Setenv(logging.DefaultLevelEnv, "")
	b := &bytes.Buffer{}
	l, err := logging.New(c, logging.WithOutput(b))
	if err != nil {
		t.Fatal(err)
	}
	return l, b
}
func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
func TestInheritance(t *testing.T) {
	l, _ := newLogger(t, logging.Config{Categories: map[string]logging.CategoryConfig{
		"rabbitmq":                {Level: ptr(logging.WarnLevel), Enabled: ptr(false)},
		"rabbitmq.consumer":       {Level: ptr(logging.DebugLevel)},
		"rabbitmq.consumer.retry": {Enabled: ptr(true)},
	}})
	for _, tc := range []struct {
		name    string
		level   logging.Level
		enabled bool
	}{
		{"http", logging.InfoLevel, true}, {"rabbitmq", logging.WarnLevel, false}, {"rabbitmq.publisher", logging.WarnLevel, false}, {"rabbitmq.consumer.worker", logging.DebugLevel, false}, {"rabbitmq.consumer.retry.delay", logging.DebugLevel, true}, {"rabbitmqx", logging.InfoLevel, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := l.Resolve(tc.name)
			if got.Level != tc.level || got.Enabled != tc.enabled {
				t.Fatalf("got %+v", got)
			}
		})
	}
}

func TestEmptyConfigDefaultsAllCategoriesToInfo(t *testing.T) {
	for _, name := range []string{"", "http", "database.query", "rabbitmq.consumer.retry", "unknown.category"} {
		t.Run(name, func(t *testing.T) {
			l, output := newLogger(t, logging.Config{})
			child := l.Category(name)
			category := name
			if category == "" {
				category = "application"
			}
			if got := l.Resolve(category); got.Level != logging.InfoLevel || !got.Enabled {
				t.Fatalf("default configuration for %q: %+v", category, got)
			}
			child.Debug("filtered")
			if output.Len() != 0 {
				t.Fatalf("debug must be filtered: %s", output.String())
			}
			child.Info("info")
			child.Warn("warn")
			child.Error("error")
			lines := strings.Split(strings.TrimSpace(output.String()), "\n")
			if len(lines) != 3 {
				t.Fatalf("expected info, warn and error: %s", output.String())
			}
			for i, level := range []string{"info", "warn", "error"} {
				var record map[string]any
				must(t, json.Unmarshal([]byte(lines[i]), &record))
				if record["level"] != level || record["category"] != category {
					t.Fatalf("unexpected entry: %+v", record)
				}
			}
		})
	}
}
func TestRuntimeUpdates(t *testing.T) {
	l, b := newLogger(t, logging.Config{})
	child := l.Category("rabbitmq").Category("consumer").With(logging.String("id", "1"))
	child.Debug("hidden")
	must(t, l.SetCategoryLevel("rabbitmq", logging.DebugLevel))
	child.Debug("visible")
	must(t, l.SetCategoryEnabled("rabbitmq", false))
	child.Error("hidden")
	must(t, l.SetCategoryEnabled("rabbitmq.consumer", true))
	child.Debug("visible")
	must(t, l.ResetCategory("rabbitmq.consumer"))
	child.Error("hidden")
	must(t, l.ResetCategory("rabbitmq"))
	must(t, l.SetDefaultLevel(logging.ErrorLevel))
	child.Warn("hidden")
	child.Error("visible")
	if strings.Contains(b.String(), "hidden") || strings.Count(b.String(), "visible") != 3 {
		t.Fatal(b.String())
	}
}
func TestJSONContract(t *testing.T) {
	l, b := newLogger(t, logging.Config{})
	l.Info("started")
	var got map[string]any
	must(t, json.Unmarshal(b.Bytes(), &got))
	if len(got) != 5 || got["category"] != "application" || got["level"] != "info" || got["msg"] != "started" || len(got["extra"].(map[string]any)) != 0 {
		t.Fatal(got)
	}
	_, err := time.Parse(logging.DefaultDateFormat, got["date"].(string))
	must(t, err)
	pos := -1
	for _, key := range []string{"date", "level", "category", "msg", "extra"} {
		next := strings.Index(b.String(), `"`+key+`":`)
		if next <= pos {
			t.Fatal(b.String())
		}
		pos = next
	}
}
func TestDateAndFields(t *testing.T) {
	l, b := newLogger(t, logging.Config{DateFormat: time.RFC3339, Timezone: "Etc/GMT-3"})
	l.Category("http").With(logging.String("request_id", "abc")).Info("done", logging.Int("status", 200), logging.Any("large", uint64(18446744073709551615)), logging.Any("nil", nil))
	var got map[string]json.RawMessage
	must(t, json.Unmarshal(b.Bytes(), &got))
	var date string
	must(t, json.Unmarshal(got["date"], &date))
	if !strings.HasSuffix(date, "+03:00") {
		t.Fatal(date)
	}
	if !strings.Contains(string(got["extra"]), "18446744073709551615") || !strings.Contains(string(got["extra"]), `"request_id":"abc"`) || !strings.Contains(string(got["extra"]), `"nil":null`) {
		t.Fatal(string(got["extra"]))
	}
}

type counted struct{ calls *atomic.Int32 }

func (v counted) MarshalLogObject(enc zapcore.ObjectEncoder) error {
	v.calls.Add(1)
	enc.AddString("password", "hidden-secret")
	enc.AddString("name", "safe")
	return nil
}
func TestFilterBeforeSerialization(t *testing.T) {
	l, b := newLogger(t, logging.Config{})
	var calls atomic.Int32
	child := l.With(logging.Object("context", counted{&calls}))
	child.Debug("hidden", logging.Object("payload", counted{&calls}))
	if calls.Load() != 0 || b.Len() != 0 {
		t.Fatal("filtered fields serialized")
	}
	child.Info("visible")
	if calls.Load() != 1 {
		t.Fatal(calls.Load())
	}
}
func TestRedaction(t *testing.T) {
	l, b := newLogger(t, logging.Config{})
	var calls atomic.Int32
	l.With(logging.String("TOKEN", "hidden-secret")).Info("safe", logging.Object("object", counted{&calls}), logging.Any("array", []any{map[string]any{"api_key": "hidden-secret", "ok": true}}), zap.Reflect("struct", struct {
		Secret string `json:"secret"`
	}{"hidden-secret"}), logging.String("msg", "extra-message"))
	if strings.Contains(b.String(), "hidden-secret") || strings.Count(b.String(), "***") != 4 {
		t.Fatal(b.String())
	}
}
func TestConfigurationIsolationAndValidation(t *testing.T) {
	level := logging.WarnLevel
	rules := map[string]logging.CategoryConfig{"a": {Level: &level}}
	l, _ := newLogger(t, logging.Config{Categories: rules})
	level = logging.DebugLevel
	delete(rules, "a")
	if l.Resolve("a").Level != logging.WarnLevel {
		t.Fatal("configuration aliased")
	}
	for _, c := range []logging.Config{{DefaultLevel: "bad"}, {Timezone: "Invalid/Zone"}, {Categories: map[string]logging.CategoryConfig{"a..b": {}}}, {Categories: map[string]logging.CategoryConfig{"a": {Level: ptr(logging.Level("bad"))}}}} {
		if _, err := logging.New(c); err == nil {
			t.Fatalf("accepted %+v", c)
		}
	}
	for _, err := range []error{l.SetDefaultLevel("bad"), l.SetCategoryLevel("a", "bad"), l.SetCategoryLevel("", logging.InfoLevel), l.SetCategoryEnabled(".a", true), l.ResetCategory("a.")} {
		if err == nil {
			t.Fatal("accepted invalid update")
		}
	}
}
func TestConcurrentUpdates(t *testing.T) {
	t.Setenv(logging.DefaultLevelEnv, "")
	l, err := logging.New(logging.Config{}, logging.WithOutput(io.Discard))
	must(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(ctx context.Context, id int) {
			defer wg.Done()
			child := l.Category(fmt.Sprintf("worker.%d", id))
			for n := 0; n < 100; n++ {
				if ctx.Err() != nil {
					return
				}
				if err := l.SetCategoryLevel(fmt.Sprintf("worker.%d", id), logging.DebugLevel); err != nil {
					t.Error(err)
					return
				}
				child.Debug("work")
				if err := l.SetDefaultLevel(logging.InfoLevel); err != nil {
					t.Error(err)
					return
				}
			}
		}(ctx, i)
	}
	wg.Wait()
	for i := 0; i < 8; i++ {
		if l.Resolve(fmt.Sprintf("worker.%d", i)).Level != logging.DebugLevel {
			t.Fatal("lost update")
		}
	}
}

type failingWriter struct{ err error }

func (w failingWriter) Write([]byte) (int, error) { return 0, w.err }
func (w failingWriter) Sync() error               { return w.err }
func TestOutputErrors(t *testing.T) {
	t.Setenv(logging.DefaultLevelEnv, "")
	sentinel := errors.New("output failed")
	var diagnostics bytes.Buffer
	l, err := logging.New(logging.Config{}, logging.WithOutput(failingWriter{sentinel}), logging.WithErrorOutput(&diagnostics))
	must(t, err)
	l.Info("test")
	if !strings.Contains(diagnostics.String(), sentinel.Error()) {
		t.Fatal(diagnostics.String())
	}
	if !errors.Is(l.Sync(), sentinel) {
		t.Fatal("lost sync error")
	}
}
func BenchmarkLogging(b *testing.B) {
	b.Setenv(logging.DefaultLevelEnv, "")
	for _, enabled := range []bool{false, true} {
		b.Run(fmt.Sprint(enabled), func(b *testing.B) {
			l, err := logging.New(logging.Config{}, logging.WithOutput(io.Discard))
			if err != nil {
				b.Fatal(err)
			}
			l = l.Category("http.request").With(logging.String("request_id", "abc"))
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if enabled {
					l.Info("done", logging.Int("status", 200))
				} else {
					l.Debug("done", logging.Int("status", 200))
				}
			}
		})
	}
}

func TestChildIsolationAndLevels(t *testing.T) {
	l, b := newLogger(t, logging.Config{DefaultLevel: logging.DebugLevel})
	base := l.With(logging.String("base", "yes"))
	left := base.With(logging.String("left", "yes"))
	right := base.With(logging.String("right", "yes"))
	left.Info("left")
	right.Info("right")
	base.Info("base")
	lines := strings.Split(strings.TrimSpace(b.String()), "\n")
	if strings.Contains(lines[0], `"right":`) || strings.Contains(lines[1], `"left":`) || strings.Contains(lines[2], `"left":`) || strings.Contains(lines[2], `"right":`) {
		t.Fatal(b.String())
	}
	b.Reset()
	l.Debug("debug")
	l.Info("info")
	l.Warn("warn")
	l.Error("error")
	l.DPanic("dpanic")
	func() {
		defer func() {
			if recover() == nil {
				t.Error("Panic did not panic")
			}
		}()
		l.Panic("panic")
	}()
	for _, level := range []string{"debug", "info", "warn", "error", "dpanic", "panic"} {
		if !strings.Contains(b.String(), `"level":"`+level+`"`) {
			t.Fatal(b.String())
		}
	}
}
func TestStacktrace(t *testing.T) {
	t.Setenv(logging.DefaultLevelEnv, "")
	var output bytes.Buffer
	l, err := logging.New(logging.Config{}, logging.WithOutput(&output), logging.WithStacktrace(logging.ErrorLevel))
	must(t, err)
	l.Error("failed")
	var record map[string]any
	must(t, json.Unmarshal(output.Bytes(), &record))
	if len(record) != 5 || !strings.Contains(record["extra"].(map[string]any)["stacktrace"].(string), "TestStacktrace") {
		t.Fatal(record)
	}
}

type shortWriter struct{}

func (shortWriter) Write(p []byte) (int, error) { return len(p) - 1, nil }
func TestShortWrite(t *testing.T) {
	t.Setenv(logging.DefaultLevelEnv, "")
	var diagnostics bytes.Buffer
	l, err := logging.New(logging.Config{}, logging.WithOutput(shortWriter{}), logging.WithErrorOutput(&diagnostics))
	must(t, err)
	l.Info("test")
	if !strings.Contains(diagnostics.String(), io.ErrShortWrite.Error()) {
		t.Fatal(diagnostics.String())
	}
}

func TestDefaultLevelFromEnvironment(t *testing.T) {
	for _, tc := range []struct {
		name       string
		env        string
		configured logging.Level
		want       logging.Level
		invalid    bool
	}{
		{name: "empty defaults to info", want: logging.InfoLevel},
		{name: "empty preserves configuration", configured: logging.WarnLevel, want: logging.WarnLevel},
		{name: "without file", env: "debug", want: logging.DebugLevel},
		{name: "overrides configuration", env: "error", configured: logging.DebugLevel, want: logging.ErrorLevel},
		{name: "normalizes case and whitespace", env: " WARN ", want: logging.WarnLevel},
		{name: "whitespace preserves configuration", env: "  ", configured: logging.ErrorLevel, want: logging.ErrorLevel},
		{name: "invalid value", env: "verbose", invalid: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(logging.DefaultLevelEnv, tc.env)
			var output bytes.Buffer
			l, err := logging.New(logging.Config{DefaultLevel: tc.configured, Categories: map[string]logging.CategoryConfig{"database": {Level: ptr(logging.WarnLevel)}}}, logging.WithOutput(&output))
			if tc.invalid {
				if err == nil {
					t.Fatal("invalid environment accepted")
				}
				return
			}
			must(t, err)
			for _, name := range []string{"application", "http.request", "unknown.child"} {
				if got := l.Resolve(name); got.Level != tc.want {
					t.Fatalf("%s: got %s, want %s", name, got.Level, tc.want)
				}
			}
			if l.Resolve("database.query").Level != logging.WarnLevel {
				t.Fatal("environment overwrote category override")
			}
			l.Debug("debug")
			if (output.Len() > 0) != (tc.want == logging.DebugLevel) {
				t.Fatalf("unexpected debug filtering: %s", output.String())
			}
			t.Setenv(logging.DefaultLevelEnv, "fatal")
			if l.Resolve("http").Level != tc.want {
				t.Fatal("environment should be read only at construction")
			}
			must(t, l.SetDefaultLevel(logging.InfoLevel))
			if l.Resolve("http").Level != logging.InfoLevel {
				t.Fatal("runtime update ignored")
			}
		})
	}
}
func TestUnsetEnvironmentDefaultsToInfo(t *testing.T) {
	t.Setenv(logging.DefaultLevelEnv, "")
	must(t, os.Unsetenv(logging.DefaultLevelEnv))
	l, err := logging.New(logging.Config{}, logging.WithOutput(io.Discard))
	must(t, err)
	if l.Resolve("anything.child").Level != logging.InfoLevel {
		t.Fatal("missing environment must default to info")
	}
}

func TestDirectArguments(t *testing.T) {
	for _, tc := range []struct {
		name   string
		values []any
		want   string
	}{
		{"error", []any{errors.New("connection refused")}, `{"error":"connection refused"}`},
		{"struct", []any{struct {
			Status int    `json:"status"`
			Token  string `json:"token"`
		}{200, "secret"}}, `{"status":200,"token":"***"}`},
		{"map", []any{map[string]any{"nested": map[string]any{"password": "secret"}, "count": uint64(18446744073709551615)}}, `{"nested":{"password":"***"},"count":18446744073709551615}`},
		{"mixed", []any{map[string]any{"status": 200}, logging.Int("status", 201), errors.New("failed")}, `{"status":201,"error":"failed"}`},
		{"array", []any{[]int{1, 2}}, `{"value":[1,2]}`},
		{"scalar", []any{"text"}, `{"value":"text"}`},
		{"nil", []any{nil}, `{"value":null}`},
		{"typed nil", []any{(*counted)(nil)}, `{"value":null}`},
		{"field slice", []any{[]logging.Field{logging.Int("status", 200)}}, `{"status":200}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			l, b := newLogger(t, logging.Config{})
			l.Info("received", tc.values...)
			var record map[string]json.RawMessage
			must(t, json.Unmarshal(b.Bytes(), &record))
			var got, want any
			decode := func(data []byte, target *any) {
				d := json.NewDecoder(bytes.NewReader(data))
				d.UseNumber()
				must(t, d.Decode(target))
			}
			decode(record["extra"], &got)
			decode([]byte(tc.want), &want)
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("got %s, want %s", record["extra"], tc.want)
			}
		})
	}
}

type jsonCounter struct{ calls *atomic.Int32 }

func (v jsonCounter) MarshalJSON() ([]byte, error) {
	v.calls.Add(1)
	return []byte(`{"token":"secret","status":200}`), nil
}
func TestDirectArgumentsAreLazy(t *testing.T) {
	l, b := newLogger(t, logging.Config{})
	var calls atomic.Int32
	child := l.With(jsonCounter{&calls})
	child.Debug("filtered", jsonCounter{&calls})
	if calls.Load() != 0 || b.Len() != 0 {
		t.Fatal("serialized filtered input")
	}
	child.Info("accepted", jsonCounter{&calls})
	if calls.Load() != 2 || strings.Contains(b.String(), "secret") {
		t.Fatalf("calls=%d output=%s", calls.Load(), b.String())
	}
}
func TestDirectObjectMarshaler(t *testing.T) {
	l, b := newLogger(t, logging.Config{})
	var calls atomic.Int32
	l.Info("object", counted{&calls})
	if calls.Load() != 1 || strings.Contains(b.String(), "hidden-secret") || !strings.Contains(b.String(), `"name":"safe"`) {
		t.Fatal(b.String())
	}
}
