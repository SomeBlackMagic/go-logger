// Package logger provides hierarchical, dynamically configurable JSON logging.
package logger

import (
	"fmt"
	"io"
	"os"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

type Logger struct {
	log   *zap.Logger
	store *configurationStore
}
type options struct {
	output     io.Writer
	errors     io.Writer
	stacktrace *Level
}
type Option func(*options)

// WithStacktrace includes a stacktrace inside extra for accepted entries at or above level.
func WithStacktrace(level Level) Option { return func(o *options) { o.stacktrace = &level } }

// WithOutput injects the destination. Writes and Sync calls are serialized.
func WithOutput(w io.Writer) Option      { return func(o *options) { o.output = w } }
func WithErrorOutput(w io.Writer) Option { return func(o *options) { o.errors = w } }
func New(config Config, opts ...Option) (*Logger, error) {
	config, err := configFromEnvironment(config, os.Getenv)
	if err != nil {
		return nil, fmt.Errorf("create logger: %w", err)
	}
	snapshot, loc, err := config.validate()
	if err != nil {
		return nil, fmt.Errorf("create logger: %w", err)
	}
	o := options{output: os.Stdout, errors: os.Stderr}
	for _, opt := range opts {
		if opt == nil {
			return nil, fmt.Errorf("nil logger option")
		}
		opt(&o)
	}
	if o.output == nil || o.errors == nil {
		return nil, fmt.Errorf("logger output must not be nil")
	}
	if config.DateFormat == "" {
		config.DateFormat = DefaultDateFormat
	}
	store := newStore(snapshot)
	core := &categoryCore{resolver: store, output: zapcore.Lock(zapcore.AddSync(o.output)), dateFormat: config.DateFormat, location: loc}
	zapOptions := []zap.Option{zap.ErrorOutput(zapcore.Lock(zapcore.AddSync(o.errors)))}
	if o.stacktrace != nil {
		if !o.stacktrace.Valid() {
			return nil, fmt.Errorf("invalid stacktrace level %q", *o.stacktrace)
		}
		zapOptions = append(zapOptions, zap.AddStacktrace(zapLevel(*o.stacktrace)))
	}
	return &Logger{log: zap.New(core, zapOptions...), store: store}, nil
}

// Category appends a dot-separated name using zap's Named semantics.
// An unnamed logger emits category application. Empty names leave it unchanged.
func (l *Logger) Category(name string) *Logger {
	return &Logger{log: l.log.Named(name), store: l.store}
}
func (l *Logger) Named(name string) *Logger { return l.Category(name) }

// With returns a derived logger retaining arbitrary values and named fields.
// It uses the same extra mapping as Info. Per-entry values override retained keys.
// Serialization is deferred until an entry is accepted. Retained values must not
// be mutated concurrently with logging; marshalers must be concurrency safe.
func (l *Logger) With(values ...any) *Logger {
	return &Logger{log: l.log.With(arguments(values)...), store: l.store}
}

// Logging methods accept named fields, errors, and arbitrary JSON-compatible values.
// Objects merge into extra; errors use extra.error; other values use extra.value.
func (l *Logger) write(level zapcore.Level, msg string, values []any) {
	if entry := l.log.Check(level, msg); entry != nil {
		entry.Write(arguments(values)...)
	}
}

// Debug logs accepted debug entries. Values use the same extra mapping as Info.
func (l *Logger) Debug(msg string, values ...any) { l.write(zapcore.DebugLevel, msg, values) }

// Info logs accepted info entries. Structs and maps producing JSON objects
// merge into extra, errors use extra.error, and other values use extra.value.
// Named fields are supported alongside direct values. Later keys override earlier
// ones. A []Field is passed as one argument, without variadic expansion.
// JSON encoding and recursive sensitive-key masking happen after filtering.
func (l *Logger) Info(msg string, values ...any) { l.write(zapcore.InfoLevel, msg, values) }

// Warn logs accepted warning entries. Values use the same extra mapping as Info.
func (l *Logger) Warn(msg string, values ...any) { l.write(zapcore.WarnLevel, msg, values) }

// Error logs accepted error entries. Pass an error directly to record its
// message in extra.error, or use the other value forms documented by Info.
func (l *Logger) Error(msg string, values ...any) { l.write(zapcore.ErrorLevel, msg, values) }

// DPanic logs at dpanic level without panicking in production mode.
// Values use the same extra mapping as Info.
func (l *Logger) DPanic(msg string, values ...any) { l.write(zapcore.DPanicLevel, msg, values) }

// Panic logs at panic level and panics even when output is disabled.
// Values use the same extra mapping as Info.
func (l *Logger) Panic(msg string, values ...any) { l.write(zapcore.PanicLevel, msg, values) }

// Fatal logs at fatal level and exits the process even when output is disabled.
// Values use the same extra mapping as Info.
func (l *Logger) Fatal(msg string, values ...any) { l.write(zapcore.FatalLevel, msg, values) }
func (l *Logger) Sync() error {
	if err := l.log.Sync(); err != nil {
		return fmt.Errorf("sync logger: %w", err)
	}
	return nil
}
