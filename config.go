package logger

import (
	"fmt"
	"time"

	"github.com/SomeBlackMagic/go-logger/internal/policy"
)

type Level = policy.Level

const (
	DebugLevel        = policy.Debug
	InfoLevel         = policy.Info
	WarnLevel         = policy.Warn
	ErrorLevel        = policy.Error
	DPanicLevel       = policy.DPanic
	PanicLevel        = policy.Panic
	FatalLevel        = policy.Fatal
	DefaultDateFormat = "2006-01-02 15:04:05"
)

type CategoryConfig = policy.CategoryConfig
type EffectiveCategoryConfig = policy.Effective

// Config's zero value enables all categories at InfoLevel, using UTC and
// DefaultDateFormat, unless LOG_LEVEL overrides the level at construction.
// Applications can pass Config{} when no config file exists.
type Config struct {
	DefaultLevel Level                     `json:"level" yaml:"level"`
	DateFormat   string                    `json:"dateFormat" yaml:"dateFormat"`
	Timezone     string                    `json:"timezone" yaml:"timezone"`
	Categories   map[string]CategoryConfig `json:"categories" yaml:"categories"`
}

func (c Config) validate() (*policy.Snapshot, *time.Location, error) {
	if c.DefaultLevel == "" {
		c.DefaultLevel = InfoLevel
	}
	if !c.DefaultLevel.Valid() {
		return nil, nil, fmt.Errorf("invalid default level %q", c.DefaultLevel)
	}
	for name, rule := range c.Categories {
		if err := policy.ValidateCategory(name); err != nil {
			return nil, nil, fmt.Errorf("configuration: %w", err)
		}
		if rule.Level != nil && !rule.Level.Valid() {
			return nil, nil, fmt.Errorf("category %q: invalid level %q", name, *rule.Level)
		}
	}
	if c.Timezone == "" {
		c.Timezone = "UTC"
	}
	loc, err := time.LoadLocation(c.Timezone)
	if err != nil {
		return nil, nil, fmt.Errorf("logging timezone: %w", err)
	}
	return (&policy.Snapshot{DefaultLevel: c.DefaultLevel, Categories: c.Categories}).Clone(), loc, nil
}
