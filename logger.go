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

// With retains fields until an accepted entry is serialized. Values must not be
// mutated concurrently with logging; object marshalers must be concurrency safe.
func (l *Logger) With(fields ...Field) *Logger {
	return &Logger{log: l.log.With(fields...), store: l.store}
}
func (l *Logger) Debug(msg string, fields ...Field)  { l.log.Debug(msg, fields...) }
func (l *Logger) Info(msg string, fields ...Field)   { l.log.Info(msg, fields...) }
func (l *Logger) Warn(msg string, fields ...Field)   { l.log.Warn(msg, fields...) }
func (l *Logger) Error(msg string, fields ...Field)  { l.log.Error(msg, fields...) }
func (l *Logger) DPanic(msg string, fields ...Field) { l.log.DPanic(msg, fields...) }

// Panic and Fatal retain zap's control-flow behavior even when output is disabled.
func (l *Logger) Panic(msg string, fields ...Field) { l.log.Panic(msg, fields...) }
func (l *Logger) Fatal(msg string, fields ...Field) { l.log.Fatal(msg, fields...) }
func (l *Logger) Sync() error {
	if err := l.log.Sync(); err != nil {
		return fmt.Errorf("sync logger: %w", err)
	}
	return nil
}
