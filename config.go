package main

import (
	"crypto/x509"
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	DefaultDomain      = "ipv4.svi.cc.cd"
	DefaultPort        = 443
	DefaultTimeout     = 300 * time.Millisecond
	DefaultConcurrency = 500
	DefaultTop         = 30
	DefaultMaxIPs      = 1_000_000
	AbsMaxIPs          = 10_000_000
	DefaultUserAgent   = "cfip-lite-mini/1.0"
	DefaultPingTimes   = 1
)

// Config holds all scanning settings.
// Priority: CLI > config.yaml > defaults.
type Config struct {
	Domain      string   `yaml:"domain"`
	IPRange     []string `yaml:"ip_range"`
	Port        int      `yaml:"port"`
	Timeout     Duration `yaml:"timeout"`
	Concurrency int      `yaml:"concurrency"`
	Top         int      `yaml:"top"`
	MaxIPs      int      `yaml:"max_ips"`
	UserAgent   string   `yaml:"user_agent"`
	HTTP        bool     `yaml:"http"`
	PingTimes   int      `yaml:"ping_times"`

	// TLSRootCAs is only used by tests to trust self-signed local servers.
	TLSRootCAs *x509.CertPool `yaml:"-"`
}

// Duration wraps time.Duration so it can be parsed from strings like "300ms"
// in YAML config files and serialized back as a human readable string.
type Duration time.Duration

func (d Duration) Duration() time.Duration { return time.Duration(d) }

func (d Duration) String() string { return time.Duration(d).String() }

// UnmarshalYAML accepts both string ("300ms") and numeric (nanoseconds) forms.
func (d *Duration) UnmarshalYAML(value *yaml.Node) error {
	var s string
	if err := value.Decode(&s); err == nil {
		parsed, perr := time.ParseDuration(s)
		if perr != nil {
			return fmt.Errorf("invalid duration %q: %w", s, perr)
		}
		*d = Duration(parsed)
		return nil
	}
	var n int64
	if err := value.Decode(&n); err == nil {
		*d = Duration(time.Duration(n))
		return nil
	}
	return fmt.Errorf("invalid duration value in config")
}

// MarshalYAML serializes the duration as a string.
func (d Duration) MarshalYAML() (interface{}, error) {
	return time.Duration(d).String(), nil
}

// DefaultConfig returns a config populated with the built-in defaults.
func DefaultConfig() *Config {
	return &Config{
		Domain:      DefaultDomain,
		Port:        DefaultPort,
		Timeout:     Duration(DefaultTimeout),
		Concurrency: DefaultConcurrency,
		Top:         DefaultTop,
		MaxIPs:      DefaultMaxIPs,
		UserAgent:   DefaultUserAgent,
		HTTP:        true,
		PingTimes:   DefaultPingTimes,
	}
}

// LoadConfigFile reads a YAML config file on top of the defaults.
func LoadConfigFile(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	cfg := DefaultConfig()
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

// Validate checks the config for obviously invalid values.
func (c *Config) Validate() error {
	if c.Domain == "" {
		return fmt.Errorf("domain must not be empty")
	}
	if c.Port <= 0 || c.Port > 65535 {
		return fmt.Errorf("invalid port %d, must be in 1..65535", c.Port)
	}
	if c.Timeout <= 0 {
		return fmt.Errorf("timeout must be positive")
	}
	if c.Concurrency <= 0 {
		return fmt.Errorf("concurrency must be positive")
	}
	if c.Top <= 0 {
		return fmt.Errorf("top must be positive")
	}
	if c.PingTimes <= 0 {
		return fmt.Errorf("ping_times must be positive")
	}
	if c.MaxIPs <= 0 {
		return fmt.Errorf("max_ips must be positive")
	}
	if c.MaxIPs > AbsMaxIPs {
		return fmt.Errorf("max_ips %d exceeds the absolute maximum of %d", c.MaxIPs, AbsMaxIPs)
	}
	return nil
}
