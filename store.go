package logger

import (
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/SomeBlackMagic/go-logger/internal/policy"
)

type configurationStore struct {
	current atomic.Pointer[policy.Snapshot]
	updates sync.Mutex
}

func newStore(s *policy.Snapshot) *configurationStore {
	store := &configurationStore{}
	store.current.Store(s)
	return store
}
func (s *configurationStore) Resolve(category string) EffectiveCategoryConfig {
	return s.current.Load().Resolve(category)
}
func (s *configurationStore) update(change func(*policy.Snapshot)) {
	s.updates.Lock()
	defer s.updates.Unlock()
	next := s.current.Load().Clone()
	change(next)
	s.current.Store(next)
}
func (l *Logger) Resolve(category string) EffectiveCategoryConfig { return l.store.Resolve(category) }
func (l *Logger) SetDefaultLevel(level Level) error {
	if !level.Valid() {
		return fmt.Errorf("invalid level %q", level)
	}
	l.store.update(func(s *policy.Snapshot) { s.DefaultLevel = level })
	return nil
}
func (l *Logger) SetCategoryLevel(category string, level Level) error {
	if err := policy.ValidateCategory(category); err != nil {
		return fmt.Errorf("set category level: %w", err)
	}
	if !level.Valid() {
		return fmt.Errorf("invalid level %q", level)
	}
	l.store.update(func(s *policy.Snapshot) { c := s.Categories[category]; c.Level = &level; s.Categories[category] = c })
	return nil
}
func (l *Logger) SetCategoryEnabled(category string, enabled bool) error {
	if err := policy.ValidateCategory(category); err != nil {
		return fmt.Errorf("set category enabled: %w", err)
	}
	l.store.update(func(s *policy.Snapshot) {
		c := s.Categories[category]
		c.Enabled = &enabled
		s.Categories[category] = c
	})
	return nil
}
func (l *Logger) ResetCategory(category string) error {
	if err := policy.ValidateCategory(category); err != nil {
		return fmt.Errorf("reset category: %w", err)
	}
	l.store.update(func(s *policy.Snapshot) { delete(s.Categories, category) })
	return nil
}
