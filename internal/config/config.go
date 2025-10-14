package config

import (
	"fmt"

	"github.com/spf13/viper"
)

// Config captures the runtime settings for ghostwire components. Service
// discovery is fully automatic; no explicit service lists are required.
type Config struct {
	Namespace         string `mapstructure:"namespace"`
	RoleLabelKey      string `mapstructure:"role_label_key"`
	RoleActive        string `mapstructure:"role_active"`
	RolePreview       string `mapstructure:"role_preview"`
	SvcPreviewPattern string `mapstructure:"svc_preview_pattern"`
	DNSSuffix         string `mapstructure:"dns_suffix"`
	NATChain          string `mapstructure:"nat_chain"`
	JumpHook          string `mapstructure:"jump_hook"`
	ExcludeCIDRs      string `mapstructure:"exclude_cidrs"`
	PollInterval      string `mapstructure:"poll_interval"`
	RefreshInterval   string `mapstructure:"refresh_interval"`
	IPv6              bool   `mapstructure:"ipv6"`
	LogLevel          string `mapstructure:"log_level"`
}

// Load reads configuration values from viper into a Config instance.
func Load() (Config, error) {
	var cfg Config
	if err := viper.Unmarshal(&cfg); err != nil {
		return Config{}, fmt.Errorf("unable to load configuration: %w", err)
	}
	return cfg, nil
}
