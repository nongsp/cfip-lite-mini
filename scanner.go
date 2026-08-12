package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"sync"
	"time"
)

// Result is a single successful probe.
type Result struct {
	IP     string   `json:"ip"`
	Status int      `json:"status"`
	Delay  Duration `json:"delay"`
	Score  int      `json:"score"`
}

// MarshalJSON renders delay as a human readable string like "38ms".
func (d Duration) MarshalJSON() ([]byte, error) {
	return json.Marshal(time.Duration(d).String())
}

// validStatus reports whether the HTTP status code counts as a hit.
func validStatus(code int) bool {
	switch code {
	case 200, 301, 302, 403:
		return true
	}
	return false
}

// scoreFor maps a status code to a simple deterministic score.
func scoreFor(code int) int {
	switch code {
	case 200:
		return 100
	case 403:
		return 90
	case 301, 302:
		return 70
	}
	return 0
}

// Scanner performs concurrent HTTPS probes.
//
// Every probe connects to IP:port while forcing TLS SNI and the HTTP Host
// header to the target domain. The IP is never resolved through DNS.
type Scanner struct {
	Domain     string
	Port       int
	Timeout    time.Duration
	UserAgent  string
	OnProgress func(scanned, total uint64)

	transport *http.Transport
	client    *http.Client
}

// NewScanner builds a Scanner from a validated config.
func NewScanner(cfg *Config) *Scanner {
	timeout := cfg.Timeout.Duration()
	dialer := &net.Dialer{Timeout: timeout}

	transport := &http.Transport{
		Proxy: nil,
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			// addr is always "ip:port"; no DNS resolution happens here.
			return dialer.DialContext(ctx, "tcp", addr)
		},
		TLSClientConfig: &tls.Config{
			ServerName: cfg.Domain,
			RootCAs:    cfg.TLSRootCAs,
		},
		TLSHandshakeTimeout:   timeout,
		ResponseHeaderTimeout: timeout,
		ExpectContinueTimeout: 1 * time.Second,
		MaxIdleConns:          4096,
		MaxIdleConnsPerHost:   32,
		IdleConnTimeout:       30 * time.Second,
		ForceAttemptHTTP2:     true,
	}

	return &Scanner{
		Domain:    cfg.Domain,
		Port:      cfg.Port,
		Timeout:   timeout,
		UserAgent: cfg.UserAgent,
		transport: transport,
		client:    &http.Client{Transport: transport, Timeout: timeout},
	}
}

// probe checks a single IP and returns its Result. Network errors, timeouts,
// TLS failures and invalid status codes simply yield a non-valid Result and
// never abort the whole scan.
func (s *Scanner) probe(ctx context.Context, ip netip.Addr) Result {
	addr := net.JoinHostPort(ip.String(), strconv.Itoa(s.Port))
	start := time.Now()

	probeCtx, cancel := context.WithTimeout(ctx, s.Timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(probeCtx, http.MethodGet, "https://"+addr+"/", nil)
	if err != nil {
		return Result{IP: ip.String(), Status: 0, Delay: Duration(time.Since(start))}
	}
	// HTTP Host header must be the target domain, never the IP.
	req.Host = s.Domain
	// Require the request to go through the real network (the transport is
	// already configured to dial ip:port and to send SNI for s.Domain).
	if s.UserAgent != "" {
		req.Header.Set("User-Agent", s.UserAgent)
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return Result{IP: ip.String(), Status: 0, Delay: Duration(time.Since(start))}
	}
	// Drain a tiny slice of the body so headers are confirmed, then close
	// immediately. We never download the whole page.
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1024))
	_ = resp.Body.Close()

	return Result{
		IP:     ip.String(),
		Status: resp.StatusCode,
		Delay:  Duration(time.Since(start)),
		Score:  scoreFor(resp.StatusCode),
	}
}

// Scan runs the full concurrent scan over src and returns the valid Results.
// All goroutines, channels and idle connections are cleaned up before return.
func (s *Scanner) Scan(ctx context.Context, src Source, concurrency int, total uint64) []Result {
	defer s.transport.CloseIdleConnections()

	ips := make(chan netip.Addr, concurrency)
	results := make(chan Result, concurrency)

	var wg sync.WaitGroup
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for ip := range ips {
				res := s.probe(ctx, ip)
				select {
				case results <- res:
				case <-ctx.Done():
					return
				}
			}
		}()
	}

	go func() {
		defer close(ips)
		for {
			ip, ok := src.Next()
			if !ok {
				return
			}
			select {
			case ips <- ip:
			case <-ctx.Done():
				return
			}
		}
	}()

	go func() {
		wg.Wait()
		close(results)
	}()

	var out []Result
	var scanned uint64
	for res := range results {
		scanned++
		if validStatus(res.Status) {
			out = append(out, res)
		}
		if s.OnProgress != nil {
			s.OnProgress(scanned, total)
		}
	}
	return out
}
