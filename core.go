package logger

import (
	"fmt"
	"io"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// The consuming adapter owns this interface; policy does not depend on zap.
type categoryResolver interface {
	Resolve(string) EffectiveCategoryConfig
}
type categoryCore struct {
	resolver   categoryResolver
	output     zapcore.WriteSyncer
	fields     []zapcore.Field
	dateFormat string
	location   *time.Location
}

// Enabled cannot inspect the category. Check performs the precise decision.
func (c *categoryCore) Enabled(level zapcore.Level) bool { return level >= zapcore.DebugLevel }
func category(name string) string {
	if name == "" {
		return "application"
	}
	return name
}
func (c *categoryCore) Check(e zapcore.Entry, ce *zapcore.CheckedEntry) *zapcore.CheckedEntry {
	rule := c.resolver.Resolve(category(e.LoggerName))
	if rule.Enabled && e.Level >= zapLevel(rule.Level) {
		return ce.AddCore(e, c)
	}
	return ce
}
func zapLevel(level Level) zapcore.Level {
	switch level {
	case DebugLevel:
		return zapcore.DebugLevel
	case WarnLevel:
		return zapcore.WarnLevel
	case ErrorLevel:
		return zapcore.ErrorLevel
	case DPanicLevel:
		return zapcore.DPanicLevel
	case PanicLevel:
		return zapcore.PanicLevel
	case FatalLevel:
		return zapcore.FatalLevel
	default:
		return zapcore.InfoLevel
	}
}
func (c *categoryCore) With(fields []zapcore.Field) zapcore.Core {
	next := *c
	next.fields = make([]zapcore.Field, 0, len(c.fields)+len(fields))
	next.fields = append(next.fields, c.fields...)
	next.fields = append(next.fields, fields...)
	return &next
}
func (c *categoryCore) Write(e zapcore.Entry, fields []zapcore.Field) error {
	if e.Stack != "" {
		fields = append(append([]zapcore.Field(nil), fields...), zap.String("stacktrace", e.Stack))
	}
	extra, err := encodeExtra(c.fields, fields)
	if err != nil {
		return fmt.Errorf("encode extra: %w", err)
	}
	enc := newJSONEncoder()
	root := []zapcore.Field{zap.String("date", e.Time.In(c.location).Format(c.dateFormat)), zap.String("level", e.Level.String()), zap.String("category", category(e.LoggerName)), zap.String("msg", e.Message), zap.Reflect("extra", extra)}
	buf, err := enc.EncodeEntry(zapcore.Entry{}, root)
	if err != nil {
		return fmt.Errorf("encode entry: %w", err)
	}
	defer buf.Free()
	n, err := c.output.Write(buf.Bytes())
	if err == nil && n != buf.Len() {
		err = io.ErrShortWrite
	}
	if err != nil {
		return fmt.Errorf("write entry: %w", err)
	}
	if e.Level > zapcore.ErrorLevel {
		return c.Sync()
	}
	return nil
}
func (c *categoryCore) Sync() error { return c.output.Sync() }
