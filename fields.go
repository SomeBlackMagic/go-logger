package logger

import (
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

type Field = zap.Field

func String(key, value string) Field                         { return zap.String(key, value) }
func Int(key string, value int) Field                        { return zap.Int(key, value) }
func Int64(key string, value int64) Field                    { return zap.Int64(key, value) }
func Bool(key string, value bool) Field                      { return zap.Bool(key, value) }
func Float64(key string, value float64) Field                { return zap.Float64(key, value) }
func Duration(key string, value time.Duration) Field         { return zap.Duration(key, value) }
func Any(key string, value any) Field                        { return zap.Any(key, value) }
func Object(key string, value zapcore.ObjectMarshaler) Field { return zap.Object(key, value) }
func Array(key string, value zapcore.ArrayMarshaler) Field   { return zap.Array(key, value) }
func Error(err error) Field                                  { return zap.Error(err) }
