package main

import (
	"testing"
	"time"
)

func TestParseCLI(t *testing.T) {
	opts, err := parseCLI([]string{
		"-domain", "tv.example.com",
		"-cidr", "43.198.0.0/16",
		"-ip", "43.198.5.166",
		"-range", "159.60.146.10-159.60.146.200",
		"-port", "443",
		"-timeout", "300ms",
	})
	if err != nil {
		t.Fatalf("parseCLI: %v", err)
	}
	if opts.domain != "tv.example.com" {
		t.Errorf("domain = %q", opts.domain)
	}
	if len(opts.cidr.values) != 1 || opts.cidr.values[0] != "43.198.0.0/16" {
		t.Errorf("cidr = %v", opts.cidr.values)
	}
	if len(opts.ip.values) != 1 || opts.ip.values[0] != "43.198.5.166" {
		t.Errorf("ip = %v", opts.ip.values)
	}
	if len(opts.rng.values) != 1 || opts.rng.values[0] != "159.60.146.10-159.60.146.200" {
		t.Errorf("range = %v", opts.rng.values)
	}
	if !opts.portSet || opts.port != 443 {
		t.Errorf("port = %d, set=%v", opts.port, opts.portSet)
	}
	if !opts.timeoutSet || opts.timeout != 300*time.Millisecond {
		t.Errorf("timeout = %v, set=%v", opts.timeout, opts.timeoutSet)
	}
}

func TestParseCLIRepeatable(t *testing.T) {
	opts, err := parseCLI([]string{
		"-domain", "d.example.com",
		"-cidr", "10.0.0.0/24",
		"-cidr", "10.0.1.0/24",
		"-ip", "1.1.1.1",
		"-ip", "2.2.2.2",
	})
	if err != nil {
		t.Fatalf("parseCLI: %v", err)
	}
	if len(opts.cidr.values) != 2 {
		t.Errorf("cidr values = %v", opts.cidr.values)
	}
	if len(opts.ip.values) != 2 {
		t.Errorf("ip values = %v", opts.ip.values)
	}
}

func TestParseCLIErrors(t *testing.T) {
	if _, err := parseCLI([]string{"-unknown-flag"}); err == nil {
		t.Fatal("expected error for unknown flag")
	}
	if _, err := parseCLI([]string{"-timeout", "notaduration"}); err == nil {
		t.Fatal("expected error for invalid duration")
	}
	if _, err := parseCLI([]string{"positional"}); err == nil {
		t.Fatal("expected error for positional argument")
	}
}

func TestParseCLIVersion(t *testing.T) {
	opts, err := parseCLI([]string{"-version"})
	if err != nil {
		t.Fatalf("parseCLI: %v", err)
	}
	if !opts.version {
		t.Fatal("version flag not set")
	}
}

func TestMergeCLIOverridesConfig(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Domain = "from-file.example.com"
	cfg.IPRange = []string{"10.1.0.0/24"}
	cfg.Port = 443
	cfg.Timeout = Duration(500 * time.Millisecond)

	opts, err := parseCLI([]string{
		"-domain", "from-cli.example.com",
		"-cidr", "10.2.0.0/24",
		"-port", "8443",
	})
	if err != nil {
		t.Fatalf("parseCLI: %v", err)
	}
	merged := opts.merge(cfg)

	if merged.Domain != "from-cli.example.com" {
		t.Errorf("Domain = %q, want CLI value", merged.Domain)
	}
	if merged.Port != 8443 {
		t.Errorf("Port = %d, want 8443", merged.Port)
	}
	if len(merged.IPRange) != 1 || merged.IPRange[0] != "10.2.0.0/24" {
		t.Errorf("IPRange = %v, want CLI value", merged.IPRange)
	}
	if merged.Timeout != Duration(500*time.Millisecond) {
		t.Errorf("Timeout = %s, config value must be kept", merged.Timeout)
	}
}

func TestMergeCLIKeepsConfigWhenNotGiven(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Domain = "keep.example.com"
	cfg.Top = 42

	opts, err := parseCLI([]string{"-domain", "d.example.com"})
	if err != nil {
		t.Fatalf("parseCLI: %v", err)
	}
	merged := opts.merge(cfg)
	if merged.Top != 42 {
		t.Errorf("Top = %d, want 42 (kept from config)", merged.Top)
	}
}

func TestMergeNoCLIRangesKeepsConfigRanges(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Domain = "d.example.com"
	cfg.IPRange = []string{"10.1.0.0/24", "10.2.0.0/24"}

	opts, err := parseCLI([]string{"-domain", "d.example.com", "-top", "5"})
	if err != nil {
		t.Fatalf("parseCLI: %v", err)
	}
	merged := opts.merge(cfg)
	if len(merged.IPRange) != 2 {
		t.Errorf("IPRange = %v, config ranges must be kept when no CLI ranges given", merged.IPRange)
	}
}
