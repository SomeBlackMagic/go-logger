package logger

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// arguments adapts input values without serializing them. With must remain lazy.
func arguments(values []any) []zap.Field {
	fields := make([]zap.Field, 0, len(values))
	for _, value := range values {
		switch v := value.(type) {
		case zap.Field:
			fields = append(fields, v)
		case []zap.Field:
			fields = append(fields, v...)
		default:
			fields = append(fields, zap.Inline(argument{value: v}))
		}
	}
	return fields
}

type argument struct{ value any }

// MarshalLogObject runs only after category filtering, inside the output adapter.
func (a argument) MarshalLogObject(enc zapcore.ObjectEncoder) error {
	if a.value == nil {
		return enc.AddReflected("value", nil)
	}
	// A typed nil error or marshaler should be encoded as null, never invoked.
	value := reflect.ValueOf(a.value)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		if value.IsNil() {
			return enc.AddReflected("value", nil)
		}
	}
	if err, ok := a.value.(error); ok {
		enc.AddString("error", err.Error())
		return nil
	}
	if object, ok := a.value.(zapcore.ObjectMarshaler); ok {
		return object.MarshalLogObject(enc)
	}
	if array, ok := a.value.(zapcore.ArrayMarshaler); ok {
		return enc.AddArray("value", array)
	}
	data, err := json.Marshal(a.value)
	if err != nil {
		return fmt.Errorf("marshal log argument: %w", err)
	}
	var decoded any
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&decoded); err != nil {
		return fmt.Errorf("decode log argument: %w", err)
	}
	if object, ok := decoded.(map[string]any); ok {
		for key, item := range object {
			if err := enc.AddReflected(key, item); err != nil {
				return fmt.Errorf("encode argument field %q: %w", key, err)
			}
		}
		return nil
	}
	return enc.AddReflected("value", decoded)
}
