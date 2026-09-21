package logger

import (
	"fmt"
	"strings"
)

// DefaultLevelEnv is read once by New. Nonempty values override Config.DefaultLevel.
const DefaultLevelEnv = "LOG_LEVEL"

// Environment access stays in the constructor adapter, outside the policy layer.
func configFromEnvironment(config Config, getenv func(string) string) (Config, error) {
	value := strings.ToLower(strings.TrimSpace(getenv(DefaultLevelEnv)))
	if value == "" {
		return config, nil
	}
	level := Level(value)
	if !level.Valid() {
		return Config{}, fmt.Errorf("invalid %s level %q", DefaultLevelEnv, value)
	}
	config.DefaultLevel = level
	return config, nil
}
