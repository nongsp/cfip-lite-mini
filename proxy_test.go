package main

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeTemp(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return path
}

func TestParseLineProxies(t *testing.T) {
	text := `
1.2.3.4
5.6.7.8:443
9.10.11.12:8443#备注注释
# 整行注释
not-an-ip
2001:db8::1
bad:port
`
	got := parseLineProxies(text)
	want := []proxyEntry{
		{IP: "1.2.3.4"},
		{IP: "5.6.7.8", Port: 443},
		{IP: "9.10.11.12", Port: 8443},
		{IP: "2001:db8::1"},
	}
	if len(got) != len(want) {
		t.Fatalf("parsed %d entries, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i].IP != want[i].IP || got[i].Port != want[i].Port {
			t.Errorf("entry %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestParseCSVProxies(t *testing.T) {
	rows := [][]string{
		{"IP 地址", "已发送", "已接收", "丢包率", "平均延迟", "下载速度(MB/s)", "地区码", "端口"},
		{"43.198.5.166", "4", "4", "0", "38", "12.3", "HKG", "443"},
		{"43.198.5.100", "4", "4", "0", "40", "11.1", "HKG", "2053"},
	}
	got, err := parseCSVProxies(rows)
	if err != nil {
		t.Fatalf("parseCSVProxies: %v", err)
	}
	want := []proxyEntry{
		{IP: "43.198.5.166", Port: 443},
		{IP: "43.198.5.100", Port: 2053},
	}
	if len(got) != len(want) {
		t.Fatalf("parsed %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("entry %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestParseCSVProxiesEnglishHeader(t *testing.T) {
	rows := [][]string{
		{"ip", "port"},
		{"43.198.5.166", "8443"},
	}
	got, err := parseCSVProxies(rows)
	if err != nil {
		t.Fatalf("parseCSVProxies: %v", err)
	}
	if len(got) != 1 || got[0].IP != "43.198.5.166" || got[0].Port != 8443 {
		t.Errorf("got %+v, want single 43.198.5.166:8443", got)
	}
}

func TestParseCSVProxiesNoIPColumn(t *testing.T) {
	rows := [][]string{
		{"host", "title"},
		{"a", "b"},
	}
	if _, err := parseCSVProxies(rows); err == nil {
		t.Fatal("expected error for CSV without IP column")
	}
}

func TestLoadProxySourceCSV(t *testing.T) {
	path := writeTemp(t, "src.csv", "IP 地址,端口\n1.2.3.4,443\n5.6.7.8,8443\n")
	got, err := loadProxySource(path)
	if err != nil {
		t.Fatalf("loadProxySource: %v", err)
	}
	if len(got) != 2 || got[1].IP != "5.6.7.8" || got[1].Port != 8443 {
		t.Fatalf("got %+v", got)
	}
}

func TestLoadProxySourcePlainList(t *testing.T) {
	path := writeTemp(t, "src.txt", "1.2.3.4\n5.6.7.8:8443\n")
	got, err := loadProxySource(path)
	if err != nil {
		t.Fatalf("loadProxySource: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d entries, want 2", len(got))
	}
}

func TestLoadProxySourceMissing(t *testing.T) {
	if _, err := loadProxySource(filepath.Join(t.TempDir(), "nope.csv")); err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestLoadProxySourceNoIPs(t *testing.T) {
	path := writeTemp(t, "empty.txt", "hello\nworld\n")
	if _, err := loadProxySource(path); err == nil {
		t.Fatal("expected error when no IPs parseable")
	}
}

func TestWriteProxyList(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ips_ports.txt")
	entries := []proxyEntry{
		{IP: "1.2.3.4", Port: 443},
		{IP: "5.6.7.8"}, // no port -> defaults to 443
		{IP: "9.10.11.12", Port: 8443},
	}
	n, err := writeProxyList(path, entries, 0)
	if err != nil {
		t.Fatalf("writeProxyList: %v", err)
	}
	if n != 3 {
		t.Fatalf("wrote %d, want 3", n)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	want := "1.2.3.4:443\n5.6.7.8:443\n9.10.11.12:8443\n"
	if string(data) != want {
		t.Errorf("file = %q, want %q", data, want)
	}
}

func TestWriteProxyListTake(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ips_ports.txt")
	entries := []proxyEntry{
		{IP: "1.2.3.4", Port: 443},
		{IP: "5.6.7.8", Port: 443},
	}
	n, err := writeProxyList(path, entries, 1)
	if err != nil {
		t.Fatalf("writeProxyList: %v", err)
	}
	if n != 1 {
		t.Fatalf("wrote %d, want 1", n)
	}
}

func TestProxySourceIteration(t *testing.T) {
	s := &proxySource{entries: []proxyEntry{
		{IP: "1.2.3.4", Port: 443},
		{IP: "5.6.7.8", Port: 8443},
	}}
	if s.Count() != 2 {
		t.Fatalf("count = %d, want 2", s.Count())
	}
	a, ok := s.Next()
	if !ok || a != netip.MustParseAddr("1.2.3.4") {
		t.Fatalf("first = %v, %v", a, ok)
	}
	a, ok = s.Next()
	if !ok || a != netip.MustParseAddr("5.6.7.8") {
		t.Fatalf("second = %v, %v", a, ok)
	}
	if _, ok := s.Next(); ok {
		t.Fatal("expected exhaustion")
	}
}

// TestProbeUsesPortMapping verifies that in proxy mode each IP dials its own
// mapped port instead of the scanner global port. A candidate on the mapped
// port succeeds while the same IP on the wrong port is rejected.
func TestProbeUsesPortMapping(t *testing.T) {
	var hits int
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	cfg := testConfig(t, ts, 500*time.Millisecond)
	cfg.HTTP = true
	s := NewScanner(cfg)
	s.PortMapping = map[string]int{"127.0.0.1": ts.Listener.Addr().(*net.TCPAddr).Port}

	res := s.probeHTTPing(context.Background(), netip.MustParseAddr("127.0.0.1"))
	if !validStatus(res.Status) || res.Status != http.StatusOK {
		t.Fatalf("mapped-port probe failed: %+v", res)
	}
	if res.Port != ts.Listener.Addr().(*net.TCPAddr).Port {
		t.Errorf("result port = %d, want mapped port", res.Port)
	}
	if hits != 1 {
		t.Errorf("server saw %d requests, want 1", hits)
	}
}

// TestProbePortMappingWrongPort verifies a candidate fails when its mapped
// port does not serve the target domain.
func TestProbePortMappingWrongPort(t *testing.T) {
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	cfg := testConfig(t, ts, 200*time.Millisecond)
	cfg.HTTP = true
	s := NewScanner(cfg)
	// Map the IP to a closed port; the probe must fail.
	s.PortMapping = map[string]int{"127.0.0.1": 1}

	res := s.probeHTTPing(context.Background(), netip.MustParseAddr("127.0.0.1"))
	if validStatus(res.Status) {
		t.Fatalf("wrong-port probe reported valid: %+v", res)
	}
}

// TestProbeHTTPingColoFilter verifies the -colo filter rejects IPs whose
// response lacks a matching region code.
func TestProbeHTTPingColoFilter(t *testing.T) {
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Server", "cloudflare")
		w.Header().Set("CF-Ray", "7bd32409eda7b020-LAX")
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	cfg := testConfig(t, ts, 500*time.Millisecond)
	cfg.HTTP = true
	cfg.PingTimes = 1

	reject := NewScanner(cfg)
	reject.Colo = "HKG,SIN"
	if res := reject.probeHTTPing(context.Background(), netip.MustParseAddr("127.0.0.1")); validStatus(res.Status) {
		t.Fatalf("LAX should be rejected by [HKG,SIN]: %+v", res)
	}

	accept := NewScanner(cfg)
	accept.Colo = "LAX"
	res := accept.probeHTTPing(context.Background(), netip.MustParseAddr("127.0.0.1"))
	if !validStatus(res.Status) {
		t.Fatalf("LAX should pass [LAX]: %+v", res)
	}
	if res.Colo != "LAX" {
		t.Errorf("colo = %q, want LAX", res.Colo)
	}
}

func TestWriteBestIPPorts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "best_ip.txt")
	results := []Result{
		{IP: "1.2.3.4", Port: 443},
		{IP: "5.6.7.8"}, // no port -> defaults to 443
	}
	n, err := WriteBestIPPorts(path, results, 2)
	if err != nil {
		t.Fatalf("WriteBestIPPorts: %v", err)
	}
	if n != 2 {
		t.Fatalf("wrote %d, want 2", n)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	want := "1.2.3.4:443\n5.6.7.8:443\n"
	if string(data) != want {
		t.Errorf("file = %q, want %q", data, want)
	}
}

// TestRunProxyGenerateOnly exercises the list-generation path of the proxy
// subcommand (no -test) end to end.
func TestRunProxyGenerateOnly(t *testing.T) {
	src := writeTemp(t, "result.csv", "IP 地址,端口\n1.2.3.4,443\n5.6.7.8,8443\n")
	outDir := t.TempDir()
	code := runProxy([]string{"-i", src, "-o", "ips_ports.txt", "-output-dir", outDir})
	if code != 0 {
		t.Fatalf("runProxy exit code = %d, want 0", code)
	}
	data, err := os.ReadFile(filepath.Join(outDir, "ips_ports.txt"))
	if err != nil {
		t.Fatalf("read list: %v", err)
	}
	want := "1.2.3.4:443\n5.6.7.8:8443\n"
	if string(data) != want {
		t.Errorf("list = %q, want %q", data, want)
	}
}
