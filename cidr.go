package main

import (
	"fmt"
	"net/netip"
	"strings"
)

// Source yields IP addresses one at a time so large CIDRs never need to be
// materialized fully in memory.
type Source interface {
	// Next returns the next address and true, or an invalid address and false
	// once the source is exhausted.
	Next() (netip.Addr, bool)
	// Count returns the estimated number of addresses in this source.
	Count() uint64
}

// ParseSource classifies an input string as CIDR, range or single IP.
func ParseSource(s string) (Source, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, fmt.Errorf("empty ip source")
	}
	switch {
	case strings.Contains(s, "/"):
		return NewCIDRSource(s)
	case strings.Contains(s, "-"):
		return NewRangeSource(s)
	default:
		return NewSingleSource(s)
	}
}

// CIDRSource iterates every address inside a CIDR prefix.
type CIDRSource struct {
	prefix  netip.Prefix
	current netip.Addr
	end     netip.Addr
}

// NewCIDRSource parses a CIDR string such as "43.198.0.0/16".
func NewCIDRSource(s string) (*CIDRSource, error) {
	p, err := netip.ParsePrefix(s)
	if err != nil {
		return nil, fmt.Errorf("invalid CIDR %q: %w", s, err)
	}
	p = p.Masked()
	if !p.Addr().Is4() && !p.Addr().Is6() {
		return nil, fmt.Errorf("invalid CIDR %q: unsupported address", s)
	}
	return &CIDRSource{
		prefix:  p,
		current: p.Addr(),
		end:     lastAddr(p),
	}, nil
}

func (s *CIDRSource) Next() (netip.Addr, bool) {
	if !s.current.IsValid() || s.current.Compare(s.end) > 0 {
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

func (s *CIDRSource) Count() uint64 {
	bits := s.prefix.Bits()
	if s.prefix.Addr().Is4() {
		if bits == 0 {
			return uint64(1) << 32
		}
		return uint64(1) << (32 - bits)
	}
	if bits <= 96 {
		return uint64(1) << 32
	}
	return uint64(1) << (128 - bits)
}

// lastAddr computes the broadcast / last address of a masked prefix.
// Host bits are filled from the least significant byte upward.
func lastAddr(p netip.Prefix) netip.Addr {
	start := p.Masked().Addr()
	if start.Is4() {
		a := start.As4()
		out := a
		rem := 32 - p.Bits()
		for i := 3; i >= 0 && rem > 0; i-- {
			if rem >= 8 {
				out[i] = 0xff
				rem -= 8
			} else {
				out[i] |= 0xff >> (8 - rem)
				rem = 0
			}
		}
		return netip.AddrFrom4(out)
	}
	a := start.As16()
	out := a
	rem := 128 - p.Bits()
	for i := 15; i >= 0 && rem > 0; i-- {
		if rem >= 8 {
			out[i] = 0xff
			rem -= 8
		} else {
			out[i] |= 0xff >> (8 - rem)
			rem = 0
		}
	}
	return netip.AddrFrom16(out)
}

// SingleSource emits exactly one address.
type SingleSource struct {
	addr netip.Addr
	done bool
}

// NewSingleSource parses a single IP string such as "43.198.5.166".
func NewSingleSource(s string) (*SingleSource, error) {
	a, err := netip.ParseAddr(s)
	if err != nil {
		return nil, fmt.Errorf("invalid IP %q: %w", s, err)
	}
	if !a.Is4() && !a.Is6() {
		return nil, fmt.Errorf("invalid IP %q: unsupported address", s)
	}
	return &SingleSource{addr: a}, nil
}

func (s *SingleSource) Next() (netip.Addr, bool) {
	if s.done {
		return netip.Addr{}, false
	}
	s.done = true
	return s.addr, true
}

func (s *SingleSource) Count() uint64 { return 1 }

// MultiSource merges several sources and enforces a global IP budget.
type MultiSource struct {
	sources []Source
	idx     int
	max     uint64
	count   uint64
}

// NewMultiSource builds a merged source scanning at most max addresses.
func NewMultiSource(sources []Source, max uint64) *MultiSource {
	return &MultiSource{sources: sources, max: max}
}

func (m *MultiSource) Next() (netip.Addr, bool) {
	for m.idx < len(m.sources) {
		if m.count >= m.max {
			return netip.Addr{}, false
		}
		a, ok := m.sources[m.idx].Next()
		if !ok {
			m.idx++
			continue
		}
		m.count++
		return a, true
	}
	return netip.Addr{}, false
}

// Count returns the estimated total number of addresses, capped at max.
func (m *MultiSource) Count() uint64 {
	var total uint64
	for _, s := range m.sources {
		c := s.Count()
		if c > m.max-total {
			return m.max
		}
		total += c
	}
	return total
}
