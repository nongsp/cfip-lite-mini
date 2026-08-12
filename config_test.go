package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Domain != DefaultDomain {
		t.Errorf("Domain = %q, want %q", cfg.Domain, DefaultDomain)
	}
	if cfg.Port != DefaultPort {
		t.Errorf("Port = %d, want %d", cfg.Port, DefaultPort)
	}
	if cfg.Timeout != Duration(DefaultTimeout) {
		t.Errorf("Timeout = %s, want %s", cfg.Timeout, DefaultTimeout)
	}
	if cfg.Concurrency != DefaultConcurrency {
		t.Errorf("Concurrency = %d, want %d", cfg.Concurrency, DefaultConcurrency)
	}
	if cfg.Top != DefaultTop {
		t.Errorf("Top = %d, want %d", cfg.Top, DefaultTop)
	}
	if cfg.MaxIPs != DefaultMaxIPs {
		t.Errorf("MaxIPs = %d, want %d", cfg.MaxIPs, DefaultMaxIPs)
	}
	if cfg.UserAgent != DefaultUserAgent {
		t.Errorf("UserAgent = %q, want %q", cfg.UserAgent, DefaultUserAgent)
	}
	if cfg.HTTP {
		t.Error("HTTP should default to false")
	}
	if cfg.PingTimes != DefaultPingTimes {
		t.Errorf("PingTimes = %d, want %d", cfg.PingTimes, DefaultPingTimes)
	}
}

func TestLoadConfigFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `domain: tv.example.com
ip_range:
  - 43.198.0.0/16
  - 159.60.146.10-159.60.146.200
  - 43.198.5.166
port: 443
timeout: 300ms
concurrency: 500
top: 30
max_ips: 1000000
user_agent: cfip-lite-mini/1.0
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	cfg, err := LoadConfigFile(path)
	if err != nil {
		t.Fatalf("LoadConfigFile: %v", err)
	}
	if cfg.Domain != "tv.example.com" {
		t.Errorf("Domain = %q", cfg.Domain)
	}
	if len(cfg.IPRange) != 3 {
		t.Errorf("IPRange length = %d, want 3", len(cfg.IPRange))
	}
	if cfg.Timeout != Duration(300*time.Millisecond) {
		t.Errorf("Timeout = %s", cfg.Timeout)
	}
	if cfg.Port != 443 || cfg.Concurrency != 500 || cfg.Top != 30 || cfg.MaxIPs != 1_000_000 {
		t.Errorf("numeric fields wrong: %+v", cfg)
	}
}

func TestLoadConfigFileDefaultsForMissingFields(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := "domain: d.example.com\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	cfg, err := LoadConfigFile(path)
	if err != nil {
		t.Fatalf("LoadConfigFile: %v", err)
	}
	if cfg.Port != DefaultPort || cfg.Timeout != Duration(DefaultTimeout) || cfg.Concurrency != DefaultConcurrency {
		t.Errorf("defaults not applied: %+v", cfg)
	}
}

func TestLoadConfigFileMissingFile(t *testing.T) {
	if _, err := LoadConfigFile(filepath.Join(t.TempDir(), "nope.yaml")); err == nil {
		t.Fatal("expected error for missing config file")
	}
}

func TestLoadConfigFileInvalidYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("domain: [unclosed"), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if _, err := LoadConfigFile(path); err == nil {
		t.Fatal("expected error for invalid YAML")
	}
}

func TestLoadConfigFileHTTPFields(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := "domain: tv.example.com\nhttp: true\nping_times: 3\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	cfg, err := LoadConfigFile(path)
	if err != nil {
		t.Fatalf("LoadConfigFile: %v", err)
	}
	if !cfg.HTTP {
		t.Error("HTTP = false, want true")
	}
	if cfg.PingTimes != 3 {
		t.Errorf("PingTimes = %d, want 3", cfg.PingTimes)
	}
}

func TestConfigValidate(t *testing.T) {
	valid := DefaultConfig()
	valid.Domain = "d.example.com"
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid config rejected: %v", err)
	}

	cfg := DefaultConfig()
	cfg.Domain = ""
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected error for empty domain")
	}

	cfg = DefaultConfig()
	cfg.Domain = "d.example.com"
	cfg.Port = 0
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected error for port 0")
	}

	cfg = DefaultConfig()
	cfg.Domain = "d.example.com"
	cfg.Timeout = 0
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected error for zero timeout")
	}

	cfg = DefaultConfig()
	cfg.Domain = "d.example.com"
	cfg.MaxIPs = AbsMaxIPs + 1
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "maximum") {
		t.Fatalf("expected max_ips cap error, got %v", err)
	}

	cfg = DefaultConfig()
	cfg.Domain = "d.example.com"
	cfg.PingTimes = 0
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected error for zero ping_times")
	}
}

func TestDurationYAML(t *testing.T) {
	if Duration(300*time.Millisecond).String() != "300ms" {
		t.Errorf("String() = %s", Duration(300*time.Millisecond))
	}
	// MarshalYAML round trip
	v, err := Duration(1500 * time.Millisecond).MarshalYAML()
	if err != nil {
		t.Fatalf("MarshalYAML: %v", err)
	}
	if v != "1.5s" {
		t.Errorf("MarshalYAML = %v", v)
	}
}
