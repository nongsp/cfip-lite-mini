package main

import (
	"net/netip"
	"testing"
)

func TestNewCIDRSourceCount(t *testing.T) {
	tests := []struct {
		cidr  string
		count uint64
	}{
		{"43.198.0.0/16", 1 << 16},
		{"43.198.5.166/32", 1},
		{"192.168.0.0/24", 256},
		{"10.0.0.0/8", 1 << 24},
		{"0.0.0.0/0", 1 << 32},
	}
	for _, tt := range tests {
		s, err := NewCIDRSource(tt.cidr)
		if err != nil {
			t.Fatalf("NewCIDRSource(%q) unexpected error: %v", tt.cidr, err)
		}
		if got := s.Count(); got != tt.count {
			t.Errorf("NewCIDRSource(%q).Count() = %d, want %d", tt.cidr, got, tt.count)
		}
	}
}

func TestCIDRSourceIteration(t *testing.T) {
	s, err := NewCIDRSource("192.168.0.0/30")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"192.168.0.0", "192.168.0.1", "192.168.0.2", "192.168.0.3"}
	var got []string
	for {
		a, ok := s.Next()
		if !ok {
			break
		}
		got = append(got, a.String())
	}
	if len(got) != len(want) {
		t.Fatalf("iterated %d addresses, want %d: %v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("address %d = %s, want %s", i, got[i], want[i])
		}
	}
}

func TestCIDRSourceIteratorExhausted(t *testing.T) {
	s, err := NewCIDRSource("192.168.0.0/30")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for i := 0; i < 4; i++ {
		if _, ok := s.Next(); !ok {
			t.Fatalf("address %d should be available", i)
		}
	}
	if _, ok := s.Next(); ok {
		t.Fatal("source should be exhausted after 4 addresses")
	}
}

func TestNewCIDRSourceInvalid(t *testing.T) {
	bad := []string{"", "not-an-ip", "43.198.0.0/33", "43.198.0.0/16/24", "999.1.1.1/8"}
	for _, c := range bad {
		if _, err := NewCIDRSource(c); err == nil {
			t.Errorf("NewCIDRSource(%q) expected error, got nil", c)
		}
	}
}

func TestNewSingleSource(t *testing.T) {
	s, err := NewSingleSource("43.198.5.166")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s.Count() != 1 {
		t.Fatalf("Count() = %d, want 1", s.Count())
	}
	a, ok := s.Next()
	if !ok || a != netip.MustParseAddr("43.198.5.166") {
		t.Fatalf("Next() = %v, %v", a, ok)
	}
	if _, ok := s.Next(); ok {
		t.Fatal("single source should be exhausted")
	}
	if _, err := NewSingleSource("invalid"); err == nil {
		t.Fatal("expected error for invalid IP")
	}
}

func TestMultiSourceMaxLimit(t *testing.T) {
	var sources []Source
	for _, c := range []string{"10.0.0.0/30", "10.0.1.0/30"} {
		s, err := NewCIDRSource(c)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		sources = append(sources, s)
	}
	m := NewMultiSource(sources, 5)
	if m.Count() != 5 {
		t.Fatalf("Count() = %d, want 5 (capped)", m.Count())
	}
	n := 0
	for {
		if _, ok := m.Next(); !ok {
			break
		}
		n++
	}
	if n != 5 {
		t.Fatalf("iterated %d addresses, want 5", n)
	}
}

func TestMultiSourceSaturatingCount(t *testing.T) {
	single, err := NewSingleSource("1.1.1.1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	wide, err := NewCIDRSource("2001:db8::/32")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	m := NewMultiSource([]Source{single, wide}, 100)
	if m.Count() != 100 {
		t.Fatalf("Count() = %d, want 100 (saturated)", m.Count())
	}
}

func TestParseSourceDispatch(t *testing.T) {
	tests := []struct {
		in   string
		kind string
	}{
		{"43.198.0.0/16", "cidr"},
		{"43.198.5.166", "single"},
		{"159.60.146.10-159.60.146.200", "range"},
	}
	for _, tt := range tests {
		src, err := ParseSource(tt.in)
		if err != nil {
			t.Fatalf("ParseSource(%q) unexpected error: %v", tt.in, err)
		}
		switch tt.kind {
		case "cidr":
			if _, ok := src.(*CIDRSource); !ok {
				t.Errorf("ParseSource(%q) is not *CIDRSource", tt.in)
			}
		case "single":
			if _, ok := src.(*SingleSource); !ok {
				t.Errorf("ParseSource(%q) is not *SingleSource", tt.in)
			}
		case "range":
			if _, ok := src.(*RangeSource); !ok {
				t.Errorf("ParseSource(%q) is not *RangeSource", tt.in)
			}
		}
	}
	if _, err := ParseSource(""); err == nil {
		t.Fatal("expected error for empty source")
	}
}
