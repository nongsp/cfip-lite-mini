package main

import (
	"context"
	"crypto/x509"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"runtime"
	"sync/atomic"
	"testing"
	"time"
)

// testDomain must be one of the SANs in Go's httptest certificate so the
// scanner's TLS verification succeeds without contacting the public internet.
const testDomain = "example.com"

// repeatingSource yields the same address n times, which lets us exercise the
// full worker-pool pipeline against a single local test server.
type repeatingSource struct {
	addr netip.Addr
	n    int
	i    int
}

func (r *repeatingSource) Next() (netip.Addr, bool) {
	if r.i >= r.n {
		return netip.Addr{}, false
	}
	r.i++
	return r.addr, true
}

func (r *repeatingSource) Count() uint64 { return uint64(r.n) }

func newTestScanner(t *testing.T, ts *httptest.Server, timeout time.Duration) *Scanner {
	t.Helper()
	pool := x509.NewCertPool()
	pool.AddCert(ts.Certificate())

	cfg := DefaultConfig()
	cfg.Domain = testDomain
	cfg.Port = ts.Listener.Addr().(*net.TCPAddr).Port
	cfg.Timeout = Duration(timeout)
	cfg.UserAgent = "cfip-lite-mini-test/1.0"
	cfg.TLSRootCAs = pool

	return NewScanner(cfg)
}

func TestProbeUsesSNIAndHost(t *testing.T) {
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host != testDomain {
			http.Error(w, "bad host: "+r.Host, 500)
			return
		}
		if r.TLS == nil || r.TLS.ServerName != testDomain {
			http.Error(w, "bad SNI: missing or wrong", 500)
			return
		}
		if r.UserAgent() != "cfip-lite-mini-test/1.0" {
			http.Error(w, "bad UA: "+r.UserAgent(), 500)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	s := newTestScanner(t, ts, 500*time.Millisecond)
	res := s.probe(context.Background(), netip.MustParseAddr("127.0.0.1"))
	if !validStatus(res.Status) || res.Status != http.StatusOK {
		t.Fatalf("probe failed, result: %+v", res)
	}
	if res.Score != 100 {
		t.Errorf("score = %d, want 100", res.Score)
	}
}

func TestProbeStatusFiltering(t *testing.T) {
	cases := []struct {
		status    int
		wantValid bool
		wantScore int
	}{
		{200, true, 100},
		{301, true, 70},
		{302, true, 70},
		{403, true, 90},
		{404, false, 0},
		{500, false, 0},
	}
	for _, c := range cases {
		status := c.status
		ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(status)
		}))
		s := newTestScanner(t, ts, 500*time.Millisecond)
		res := s.probe(context.Background(), netip.MustParseAddr("127.0.0.1"))
		if validStatus(res.Status) != c.wantValid {
			t.Errorf("status %d: valid = %v, want %v", status, validStatus(res.Status), c.wantValid)
		}
		if res.Score != c.wantScore {
			t.Errorf("status %d: score = %d, want %d", status, res.Score, c.wantScore)
		}
		ts.Close()
	}
}

func TestProbeTimeout(t *testing.T) {
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(500 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	// probe timeout far below the handler latency: the IP must be discarded.
	s := newTestScanner(t, ts, 50*time.Millisecond)
	res := s.probe(context.Background(), netip.MustParseAddr("127.0.0.1"))
	if validStatus(res.Status) {
		t.Fatalf("timed out probe reported valid result: %+v", res)
	}
}

func TestScanConcurrencyAndCount(t *testing.T) {
	var hits atomic.Int32
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	s := newTestScanner(t, ts, 500*time.Millisecond)
	src := &repeatingSource{addr: netip.MustParseAddr("127.0.0.1"), n: 200}

	before := runtime.NumGoroutine()
	results := s.Scan(context.Background(), src, 16, 200)

	// Transport connections close asynchronously; poll until workers and
	// connection goroutines have exited.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if runtime.NumGoroutine() <= before+5 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	after := runtime.NumGoroutine()

	if len(results) != 200 {
		t.Fatalf("got %d results, want 200", len(results))
	}
	if hits.Load() != 200 {
		t.Errorf("server saw %d requests, want 200", hits.Load())
	}
	for i, r := range results {
		if r.IP != "127.0.0.1" || r.Status != http.StatusOK {
			t.Fatalf("result %d = %+v", i, r)
		}
	}
	if after > before+5 {
		t.Errorf("possible goroutine leak: before=%d after=%d", before, after)
	}
}

func TestScanTimeoutDiscardsSlowIPs(t *testing.T) {
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(400 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	s := newTestScanner(t, ts, 50*time.Millisecond)
	src := &repeatingSource{addr: netip.MustParseAddr("127.0.0.1"), n: 10}

	start := time.Now()
	results := s.Scan(context.Background(), src, 4, 10)
	elapsed := time.Since(start)

	if len(results) != 0 {
		t.Fatalf("got %d results, want 0 (all should time out)", len(results))
	}
	if elapsed > 3*time.Second {
		t.Errorf("scan took %v, timeout not respected", elapsed)
	}
}

func TestScanContextCancellation(t *testing.T) {
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(5 * time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	s := newTestScanner(t, ts, 200*time.Millisecond)
	src := &repeatingSource{addr: netip.MustParseAddr("127.0.0.1"), n: 10000}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan []Result, 1)
	go func() {
		done <- s.Scan(ctx, src, 8, 10000)
	}()

	time.Sleep(100 * time.Millisecond)
	cancel()

	select {
	case <-done:
		// scan returned promptly after cancellation
	case <-time.After(5 * time.Second):
		t.Fatal("cancelled scan did not return promptly")
	}
}

func TestScanEmptySource(t *testing.T) {
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	s := newTestScanner(t, ts, 500*time.Millisecond)
	src := &repeatingSource{addr: netip.MustParseAddr("127.0.0.1"), n: 0}
	results := s.Scan(context.Background(), src, 4, 0)
	if len(results) != 0 {
		t.Fatalf("got %d results for empty source", len(results))
	}
}

func TestProgressCallback(t *testing.T) {
	var calls atomic.Int64
	var lastScanned atomic.Uint64
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	s := newTestScanner(t, ts, 500*time.Millisecond)
	s.OnProgress = func(scanned, total uint64) {
		calls.Add(1)
		lastScanned.Store(scanned)
	}
	src := &repeatingSource{addr: netip.MustParseAddr("127.0.0.1"), n: 50}
	results := s.Scan(context.Background(), src, 8, 50)
	if len(results) != 50 {
		t.Fatalf("got %d results, want 50", len(results))
	}
	if calls.Load() == 0 {
		t.Fatal("progress callback never called")
	}
	if lastScanned.Load() != 50 {
		t.Errorf("last scanned = %d, want 50", lastScanned.Load())
	}
}
