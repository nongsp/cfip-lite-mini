package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
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
	Port   int      `json:"port,omitempty"`
	Status int      `json:"status"`
	Delay  Duration `json:"delay"`
	Score  int      `json:"score"`
	Colo   string   `json:"colo,omitempty"`
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
	HTTP       bool
	PingTimes  int
	TLSRootCAs *x509.CertPool
	// PortMapping overrides the target port per IP (proxy mode): each
	// candidate from a user-supplied IP:port list carries its own port.
	PortMapping map[string]int
	// Colo is a comma separated list of expected colo codes (proxy mode).
	// When set, probes whose response headers do not carry a matching colo
	// are rejected. Mirrors yx-tools' -cfcolo filtering.
	Colo string
	// OnProgress reports scan progress.
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

	pingTimes := cfg.PingTimes
	if pingTimes < 1 {
		pingTimes = 1
	}

	return &Scanner{
		Domain:      cfg.Domain,
		Port:        cfg.Port,
		Timeout:     timeout,
		UserAgent:   cfg.UserAgent,
		HTTP:        cfg.HTTP,
		PingTimes:   pingTimes,
		TLSRootCAs:  cfg.TLSRootCAs,
		PortMapping: make(map[string]int),
		transport:   transport,
		// Stop redirects in every mode: following a redirect would re-dial the
		// Location host and defeat the enforced SNI/Host (the same approach as
		// yx-tools' -http scanning method).
		client: &http.Client{
			Transport:     transport,
			Timeout:       timeout,
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
	}
}

// portFor returns the per-IP port from PortMapping when present (proxy mode),
// falling back to the scanner's global port.
func (s *Scanner) portFor(ip netip.Addr) int {
	if p, ok := s.PortMapping[ip.String()]; ok && p > 0 && p < 65536 {
		return p
	}
	return s.Port
}

// rejectsColo reports whether the extracted colo fails the configured filter.
func (s *Scanner) rejectsColo(colo string) bool {
	if s.Colo == "" {
		return false
	}
	return !matchColo(splitColos(s.Colo), colo)
}

// newRequest builds the HTTPS request that always targets IP:port while
// forcing the HTTP Host header to the target domain. yx-tools' -http method
// uses HEAD requests, which are full HTTP exchanges (TLS handshake + server
// response) without transferring the body.
func (s *Scanner) newRequest(ctx context.Context, addr string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, "https://"+addr+"/", nil)
	if err != nil {
		return nil, err
	}
	req.Host = s.Domain
	if s.UserAgent != "" {
		req.Header.Set("User-Agent", s.UserAgent)
	}
	return req, nil
}

// probe checks a single IP using the shared transport (default mode).
// Network errors, timeouts, TLS failures and invalid status codes simply
// yield a non-valid Result and never abort the whole scan.
func (s *Scanner) probe(ctx context.Context, ip netip.Addr) Result {
	addr := net.JoinHostPort(ip.String(), strconv.Itoa(s.portFor(ip)))
	start := time.Now()

	probeCtx, cancel := context.WithTimeout(ctx, s.Timeout)
	defer cancel()

	req, err := s.newRequest(probeCtx, addr)
	if err != nil {
		return Result{IP: ip.String(), Status: 0, Delay: Duration(time.Since(start))}
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return Result{IP: ip.String(), Status: 0, Delay: Duration(time.Since(start))}
	}
	// Drain a tiny slice of the body so headers are confirmed, then close
	// immediately. We never download the whole page.
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1024))
	_ = resp.Body.Close()

	colo := getHeaderColo(resp.Header)
	if s.rejectsColo(colo) {
		return Result{IP: ip.String(), Status: 0, Delay: Duration(time.Since(start))}
	}

	return Result{
		IP:     ip.String(),
		Port:   s.portFor(ip),
		Status: resp.StatusCode,
		Delay:  Duration(time.Since(start)),
		Score:  scoreFor(resp.StatusCode),
		Colo:   colo,
	}
}

// probeHTTPing checks a single IP using the yx-tools -http scanning method:
//
//   - a dedicated transport per IP, dialing IP:port, torn down with
//     CloseIdleConnections as soon as the probe finishes;
//   - SetLinger(0) so TCP connections close with RST instead of lingering in
//     TIME_WAIT, which would otherwise exhaust the local port pool when
//     scanning thousands of candidates;
//   - redirects stopped via CheckRedirect, so a 301/302 is reported as-is and
//     the connection is never re-dialed to the Location host;
//   - the latency is averaged over PingTimes requests per IP.
func (s *Scanner) probeHTTPing(ctx context.Context, ip netip.Addr) Result {
	addr := net.JoinHostPort(ip.String(), strconv.Itoa(s.portFor(ip)))

	tr := &http.Transport{
		Proxy: nil,
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", address)
			if err != nil {
				return nil, err
			}
			if tc, ok := conn.(*net.TCPConn); ok {
				_ = tc.SetLinger(0)
			}
			return conn, nil
		},
		TLSClientConfig: &tls.Config{
			ServerName: s.Domain,
			RootCAs:    s.TLSRootCAs,
		},
		TLSHandshakeTimeout:   s.Timeout,
		ResponseHeaderTimeout: s.Timeout,
		DisableKeepAlives:     true,
		ForceAttemptHTTP2:     true,
	}
	defer tr.CloseIdleConnections()

	hc := &http.Client{
		Transport: tr,
		Timeout:   s.Timeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	statusCode := 0
	colo := ""
	success := 0
	var totalDelay time.Duration
	var start time.Time

	for i := 0; i < s.PingTimes; i++ {
		probeCtx, cancel := context.WithTimeout(ctx, s.Timeout)
		start = time.Now()

		req, err := s.newRequest(probeCtx, addr)
		if err != nil {
			cancel()
			break
		}
		if i == s.PingTimes-1 {
			req.Header.Set("Connection", "close")
		}

		resp, err := hc.Do(req)
		if err != nil {
			cancel()
			if i == 0 {
				break
			}
			continue
		}
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1024))
		_ = resp.Body.Close()
		cancel()

		if i == 0 {
			if !validStatus(resp.StatusCode) {
				return Result{IP: ip.String(), Status: resp.StatusCode, Delay: Duration(time.Since(start))}
			}
			statusCode = resp.StatusCode
			colo = getHeaderColo(resp.Header)
			// yx-tools rejects an IP as soon as its colo does not match the
			// requested -cfcolo list, no further requests are sent.
			if s.rejectsColo(colo) {
				return Result{IP: ip.String(), Status: 0, Delay: Duration(time.Since(start))}
			}
		}
		success++
		totalDelay += time.Since(start)
	}

	if success == 0 || statusCode == 0 {
		return Result{IP: ip.String(), Status: 0, Delay: Duration(time.Since(start))}
	}
	return Result{
		IP:     ip.String(),
		Port:   s.portFor(ip),
		Status: statusCode,
		Delay:  Duration(totalDelay / time.Duration(success)),
		Score:  scoreFor(statusCode),
		Colo:   colo,
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
				var res Result
				if s.HTTP {
					res = s.probeHTTPing(ctx, ip)
				} else {
					res = s.probe(ctx, ip)
				}
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
