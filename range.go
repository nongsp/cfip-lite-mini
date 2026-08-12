package main

import (
	"fmt"
	"net/netip"
	"strings"
)

// RangeSource iterates every address between start and end, inclusive.
type RangeSource struct {
	start   netip.Addr
	end     netip.Addr
	current netip.Addr
}

// NewRangeSource parses an inclusive range such as
// "159.60.146.10-159.60.146.200".
func NewRangeSource(s string) (*RangeSource, error) {
	parts := strings.SplitN(s, "-", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid IP range %q, expected format start-end", s)
	}
	startStr := strings.TrimSpace(parts[0])
	endStr := strings.TrimSpace(parts[1])
	start, err := netip.ParseAddr(startStr)
	if err != nil {
		return nil, fmt.Errorf("invalid range start %q: %w", startStr, err)
	}
	end, err := netip.ParseAddr(endStr)
	if err != nil {
		return nil, fmt.Errorf("invalid range end %q: %w", endStr, err)
	}
	if start.Is4() != end.Is4() {
		return nil, fmt.Errorf("invalid range %q: start and end must use the same address family", s)
	}
	if end.Compare(start) < 0 {
		return nil, fmt.Errorf("invalid range %q: end is before start", s)
	}
	return &RangeSource{start: start, end: end, current: start}, nil
}

func (s *RangeSource) Next() (netip.Addr, bool) {
	if !s.current.IsValid() {
		return netip.Addr{}, false
	}
	a := s.current
	if s.current == s.end {
		s.current = netip.Addr{}
	} else {
		s.current = s.current.Next()
	}
	return a, true
}

func (s *RangeSource) Count() uint64 { return rangeCount(s.start, s.end) }

// rangeCount returns end-start+1, saturating on huge IPv6 ranges.
func rangeCount(start, end netip.Addr) uint64 {
	if start.Is4() {
		s := uint64(start.As4()[3]) | uint64(start.As4()[2])<<8 | uint64(start.As4()[1])<<16 | uint64(start.As4()[0])<<24
		e := uint64(end.As4()[3]) | uint64(end.As4()[2])<<8 | uint64(end.As4()[1])<<16 | uint64(end.As4()[0])<<24
		return e - s + 1
	}
	sa := start.As16()
	ea := end.As16()
	for i := 0; i < 16; i++ {
		if sa[i] != ea[i] {
			if i < 8 {
				return ^uint64(0)
			}
			var sLow, eLow uint64
			for j := 0; j < 8; j++ {
				sLow = sLow<<8 | uint64(sa[8+j])
				eLow = eLow<<8 | uint64(ea[8+j])
			}
			return eLow - sLow + 1
		}
	}
	return 1
}
