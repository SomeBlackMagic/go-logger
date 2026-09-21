package logger

import (
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/SomeBlackMagic/go-logger/internal/policy"
)

// configurationStore publishes immutable snapshots. Updates are serialized to
// prevent lost changes; readers never acquire the update mutex.
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

// Resolve returns the effective level and enabled state for an absolute category
// name. Level and enabled overrides inherit independently. It is safe to call
// concurrently with logging and configuration updates.
func (l *Logger) Resolve(category string) EffectiveCategoryConfig { return l.store.Resolve(category) }

// SetDefaultLevel replaces the fallback level for this logger and all its derived
// loggers, including existing instances. It supersedes the value read from
// LOG_LEVEL at construction without changing explicit category overrides.
// Invalid levels return an error and leave the configuration unchanged.
func (l *Logger) SetDefaultLevel(level Level) error {
	if !level.Valid() {
		return fmt.Errorf("invalid level %q", level)
	}
	l.store.update(func(s *policy.Snapshot) { s.DefaultLevel = level })
	return nil
}

// SetCategoryLevel sets an absolute category's level override, preserving its
// enabled override. Descendants inherit the level unless they override it.
// Invalid input returns an error without changing the configuration.
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

// SetCategoryEnabled sets an absolute category's enabled override, preserving its
// level override. Descendants inherit this state unless they override it; true
// can re-enable a child of a disabled category. Invalid names return an error.
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

// ResetCategory removes both local overrides for an absolute category name.
// It restores inheritance from ancestors or the current default level, without
// removing descendant overrides. Resetting an absent valid category succeeds.
// All update methods are concurrency safe and publish one complete snapshot;
// entries already accepted for logging can finish writing after an update.
func (l *Logger) ResetCategory(category string) error {
	if err := policy.ValidateCategory(category); err != nil {
		return fmt.Errorf("reset category: %w", err)
	}
	l.store.update(func(s *policy.Snapshot) { delete(s.Categories, category) })
	return nil
}
