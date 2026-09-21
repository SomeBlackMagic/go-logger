// Package policy defines logging rules without dependencies on logging infrastructure.
package policy

import (
	"fmt"
	"strings"
)

type Level string

const (
	Debug  Level = "debug"
	Info   Level = "info"
	Warn   Level = "warn"
	Error  Level = "error"
	DPanic Level = "dpanic"
	Panic  Level = "panic"
	Fatal  Level = "fatal"
)

func (l Level) Valid() bool {
	switch l {
	case Debug, Info, Warn, Error, DPanic, Panic, Fatal:
		return true
	}
	return false
}
func ValidateCategory(s string) error {
	if s == "" || strings.TrimSpace(s) != s {
		return fmt.Errorf("invalid category %q", s)
	}
	for _, part := range strings.Split(s, ".") {
		if part == "" || strings.TrimSpace(part) != part {
			return fmt.Errorf("invalid category %q", s)
		}
	}
	return nil
}

type CategoryConfig struct {
	Level   *Level `json:"level,omitempty" yaml:"level,omitempty"`
	Enabled *bool  `json:"enabled,omitempty" yaml:"enabled,omitempty"`
}
type Effective struct {
	Level   Level
	Enabled bool
}
type Snapshot struct {
	DefaultLevel Level
	Categories   map[string]CategoryConfig
}

func (s *Snapshot) Resolve(category string) Effective {
	result := Effective{Level: s.DefaultLevel, Enabled: true}
	levelFound, enabledFound := false, false
	for category != "" {
		if c, ok := s.Categories[category]; ok {
			if !levelFound && c.Level != nil {
				result.Level = *c.Level
				levelFound = true
			}
			if !enabledFound && c.Enabled != nil {
				result.Enabled = *c.Enabled
				enabledFound = true
			}
		}
		if levelFound && enabledFound {
			break
		}
		i := strings.LastIndexByte(category, '.')
		if i < 0 {
			break
		}
		category = category[:i]
	}
	return result
}
func (s *Snapshot) Clone() *Snapshot {
	out := &Snapshot{DefaultLevel: s.DefaultLevel, Categories: make(map[string]CategoryConfig, len(s.Categories))}
	for k, v := range s.Categories {
		if v.Level != nil {
			l := *v.Level
			v.Level = &l
		}
		if v.Enabled != nil {
			b := *v.Enabled
			v.Enabled = &b
		}
		out.Categories[k] = v
	}
	return out
}
