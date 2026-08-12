package main

import (
	"net/netip"
	"testing"
)

func TestNewRangeSourceIteration(t *testing.T) {
	s, err := NewRangeSource("10.0.0.1-10.0.0.4")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s.Count() != 4 {
		t.Fatalf("Count() = %d, want 4", s.Count())
	}
	want := []string{"10.0.0.1", "10.0.0.2", "10.0.0.3", "10.0.0.4"}
	var got []string
	for {
		a, ok := s.Next()
		if !ok {
			break
		}
		got = append(got, a.String())
	}
	if len(got) != len(want) {
		t.Fatalf("iterated %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("address %d = %s, want %s", i, got[i], want[i])
		}
	}
	if _, ok := s.Next(); ok {
		t.Fatal("range source should be exhausted")
	}
}

func TestRangeSingleValue(t *testing.T) {
	s, err := NewRangeSource("43.198.5.166-43.198.5.166")
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
}

func TestRangeCount(t *testing.T) {
	start := netip.MustParseAddr("159.60.146.10")
	end := netip.MustParseAddr("159.60.146.200")
	if got := rangeCount(start, end); got != 191 {
		t.Errorf("rangeCount = %d, want 191", got)
	}
	if got := rangeCount(start, start); got != 1 {
		t.Errorf("rangeCount(same) = %d, want 1", got)
	}
}

func TestNewRangeSourceInvalid(t *testing.T) {
	bad := []string{
		"10.0.0.1",             // missing dash
		"10.0.0.1-",            // empty end
		"-10.0.0.1",            // empty start
		"10.0.0.9-10.0.0.1",    // reversed
		"10.0.0.1-2001:db8::1", // mixed families
		"not-an-ip-10.0.0.1",   // invalid start
		"10.0.0.1-not-an-ip",   // invalid end
	}
	for _, r := range bad {
		if _, err := NewRangeSource(r); err == nil {
			t.Errorf("NewRangeSource(%q) expected error, got nil", r)
		}
	}
}
