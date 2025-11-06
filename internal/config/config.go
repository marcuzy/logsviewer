package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/viper"
)

const (
	defaultTimestampField = "timestamp"
	defaultMessageField   = "message"
)

// Config represents the merged application configuration.
type Config struct {
	Files          []string `mapstructure:"files"`
	TailLines      int      `mapstructure:"tail_lines"`
	MaxEntries     int      `mapstructure:"max_entries"`
	TimestampField string   `mapstructure:"timestamp_field"`
	MessageField   string   `mapstructure:"message_field"`
	ExtraFields    []string `mapstructure:"extra_fields"`
}

// Flags captures CLI overrides supplied by the user.
type Flags struct {
	ConfigPath     string
	Files          []string
	TailLines      *int
	MaxEntries     *int
	TimestampField string
	MessageField   string
	ExtraFields    []string
}

// Load resolves configuration from defaults, config files, and CLI overrides.
func Load(flags Flags) (Config, error) {
	v := viper.New()
	setDefaults(v)

	if flags.ConfigPath != "" {
		v.SetConfigFile(flags.ConfigPath)
	} else {
		addDefaultConfigPaths(v)
		v.SetConfigName("logsviewer")
	}

	if err := readConfig(v); err != nil {
		return Config{}, err
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return Config{}, fmt.Errorf("decode config: %w", err)
	}

	cfg = applyOverrides(cfg, flags)
	cfg = ensureDefaults(cfg)

	if len(cfg.Files) == 0 {
		return Config{}, fmt.Errorf("no log files configured; set via config file or --file flag")
	}

	return cfg, nil
}

func setDefaults(v *viper.Viper) {
	v.SetDefault("tail_lines", 200)
	v.SetDefault("max_entries", 1000)
	v.SetDefault("timestamp_field", defaultTimestampField)
	v.SetDefault("message_field", defaultMessageField)
	v.SetDefault("extra_fields", []string{"level"})
}

func addDefaultConfigPaths(v *viper.Viper) {
	v.AddConfigPath(".")

	if home := homeDir(); home != "" {
		v.AddConfigPath(filepath.Join(home, ".config", "logsviewer"))
		v.AddConfigPath(filepath.Join(home, ".logsviewer"))
	}
}

func readConfig(v *viper.Viper) error {
	if err := v.ReadInConfig(); err != nil {
		var configFileNotFound viper.ConfigFileNotFoundError
		if errors.As(err, &configFileNotFound) {
			return nil
		}
		return fmt.Errorf("read config: %w", err)
	}
	return nil
}

func applyOverrides(cfg Config, flags Flags) Config {
	if len(flags.Files) > 0 {
		cfg.Files = uniquePaths(flags.Files)
	}
	if flags.TailLines != nil {
		cfg.TailLines = *flags.TailLines
	}
	if flags.MaxEntries != nil {
		cfg.MaxEntries = *flags.MaxEntries
	}
	if flags.TimestampField != "" {
		cfg.TimestampField = flags.TimestampField
	}
	if flags.MessageField != "" {
		cfg.MessageField = flags.MessageField
	}
	if len(flags.ExtraFields) > 0 {
		cfg.ExtraFields = flags.ExtraFields
	}
	return cfg
}

func ensureDefaults(cfg Config) Config {
	if cfg.TimestampField == "" {
		cfg.TimestampField = defaultTimestampField
	}
	if cfg.MessageField == "" {
		cfg.MessageField = defaultMessageField
	}
	if len(cfg.ExtraFields) == 0 {
		cfg.ExtraFields = []string{"level"}
	}
	if cfg.MaxEntries <= 0 {
		cfg.MaxEntries = 1000
	}
	if cfg.TailLines < 0 {
		cfg.TailLines = 0
	}
	return cfg
}

func uniquePaths(in []string) []string {
	seen := make(map[string]struct{})
	var out []string
	for _, p := range in {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if _, ok := seen[p]; ok {
			continue
		}
		seen[p] = struct{}{}
		out = append(out, p)
	}
	return out
}

func homeDir() string {
	if dir, err := os.UserHomeDir(); err == nil {
		return dir
	}
	return ""
}
