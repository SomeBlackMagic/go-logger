package logger

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"go.uber.org/zap/zapcore"
)

func newJSONEncoder() zapcore.Encoder {
	return zapcore.NewJSONEncoder(zapcore.EncoderConfig{LineEnding: "\n", EncodeDuration: zapcore.StringDurationEncoder, EncodeTime: zapcore.ISO8601TimeEncoder})
}

// Normalize through JSON so reflected values, custom object marshalers and arrays
// all pass through the same recursive redaction boundary. UseNumber preserves integers.
func encodeExtra(context, fields []zapcore.Field) (map[string]any, error) {
	enc := newJSONEncoder()
	for _, group := range [][]zapcore.Field{context, fields} {
		for _, field := range group {
			if sensitive(field.Key) {
				enc.AddString(field.Key, "***")
			} else {
				field.AddTo(enc)
			}
		}
	}
	buf, err := enc.EncodeEntry(zapcore.Entry{}, nil)
	if err != nil {
		return nil, fmt.Errorf("encode fields: %w", err)
	}
	defer buf.Free()
	result := make(map[string]any)
	dec := json.NewDecoder(bytes.NewReader(buf.Bytes()))
	dec.UseNumber()
	if err = dec.Decode(&result); err != nil {
		return nil, fmt.Errorf("decode fields: %w", err)
	}
	redact(result)
	return result, nil
}
func sensitive(key string) bool {
	switch strings.ToLower(key) {
	case "password", "passwd", "token", "access_token", "refresh_token", "authorization", "cookie", "secret", "api_key":
		return true
	}
	return false
}
func redact(value any) {
	switch v := value.(type) {
	case map[string]any:
		for k, x := range v {
			if sensitive(k) {
				v[k] = "***"
			} else {
				redact(x)
			}
		}
	case []any:
		for _, x := range v {
			redact(x)
		}
	}
}
