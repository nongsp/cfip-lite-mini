package main

import (
	"context"
	"encoding/csv"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/netip"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// proxyEntry is a single candidate extracted from a proxy source file.
type proxyEntry struct {
	IP   string
	Port int
}

// proxySource iterates a fixed list of proxy IPs. It implements Source so the
// existing worker-pool Scan pipeline is reused unchanged.
type proxySource struct {
	entries []proxyEntry
	idx     int
}

func (p *proxySource) Next() (netip.Addr, bool) {
	if p.idx >= len(p.entries) {
		return netip.Addr{}, false
	}
	a, err := netip.ParseAddr(p.entries[p.idx].IP)
	if err != nil {
		p.idx++
		return p.Next()
	}
	p.idx++
	return a, true
}

func (p *proxySource) Count() uint64 { return uint64(len(p.entries)) }

// proxyCSVColumns locates the IP and port columns in a CSV header row.
// Header names are normalized (BOM stripped, lower case, spaces removed) so
// both yx-tools style "IP 地址"/"端口" and asset-export style "ip"/"port"
// headers are recognized.
func proxyCSVColumns(header []string) (ipCol, portCol int) {
	ipCol, portCol = -1, -1
	for i, h := range header {
		h = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(h)), "\ufeff")
		h = strings.ReplaceAll(h, " ", "")
		switch h {
		case "ip", "ipaddress", "ip地址", "ipaddr":
			ipCol = i
		case "port", "端口":
			portCol = i
		}
	}
	return
}

// parseCSVProxies parses a CSV with ip/port columns into candidates.
func parseCSVProxies(rows [][]string) ([]proxyEntry, error) {
	if len(rows) < 2 {
		return nil, fmt.Errorf("CSV 没有数据行")
	}
	ipCol, portCol := proxyCSVColumns(rows[0])
	if ipCol < 0 {
		return nil, fmt.Errorf("CSV 首行找不到 IP 列（支持表头：ip / IP 地址）")
	}
	var out []proxyEntry
	for _, row := range rows[1:] {
		if ipCol >= len(row) {
			continue
		}
		ip := strings.TrimSpace(row[ipCol])
		if ip == "" {
			continue
		}
		port := 0
		if portCol >= 0 && portCol < len(row) {
			if p, err := strconv.Atoi(strings.TrimSpace(row[portCol])); err == nil && p > 0 && p < 65536 {
				port = p
			}
		}
		out = append(out, proxyEntry{IP: ip, Port: port})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("CSV 里没有可用的 IP")
	}
	return out, nil
}

// parseLineProxies parses a plain list where every line is "IP[:port]" or
// "IP:port#comment" (GitHub style notes). Ports default to 0, which the
// caller resolves via -port.
func parseLineProxies(text string) []proxyEntry {
	var out []proxyEntry
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if i := strings.IndexByte(line, '#'); i >= 0 {
			line = strings.TrimSpace(line[:i])
		}
		if line == "" {
			continue
		}
		port := 0
		ip := line
		// "IP:port" only when exactly two colon-separated parts exist, so a
		// compressed IPv6 like 2001:db8::1 is never split into ip/port.
		if strings.Count(line, ":") == 1 {
			parts := strings.SplitN(line, ":", 2)
			if p, err := strconv.Atoi(strings.TrimSpace(parts[1])); err == nil && p > 0 && p < 65536 {
				ip = strings.TrimSpace(parts[0])
				port = p
			}
		}
		if _, err := netip.ParseAddr(ip); err != nil {
			continue
		}
		out = append(out, proxyEntry{IP: ip, Port: port})
	}
	return out
}

// loadProxySource reads a proxy source file: a CSV with ip/port columns or a
// plain per-line IP:port list. CSV is attempted first, then the plain list.
func loadProxySource(path string) ([]proxyEntry, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	r := csv.NewReader(f)
	r.FieldsPerRecord = -1
	rows, err := r.ReadAll()
	if err == nil && len(rows) >= 2 {
		if _, ipCol := proxyCSVColumns(rows[0]); ipCol >= 0 {
			return parseCSVProxies(rows)
		}
	}

	// Not a CSV (or headerless): re-read as plain lines.
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	entries := parseLineProxies(string(data))
	if len(entries) == 0 {
		return nil, fmt.Errorf("%s 里没有可用的 IP（每行应为 IP 或 IP:端口）", path)
	}
	return entries, nil
}

// writeProxyList writes candidates as "IP:port" lines, taking the first limit
// entries when limit > 0. Returns the number of entries written.
func writeProxyList(path string, entries []proxyEntry, limit int) (int, error) {
	if limit > 0 && limit < len(entries) {
		entries = entries[:limit]
	}
	f, err := os.Create(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	for _, e := range entries {
		port := e.Port
		if port <= 0 {
			port = DefaultPort
		}
		if _, err := fmt.Fprintf(f, "%s:%d\n", e.IP, port); err != nil {
			return 0, err
		}
	}
	return len(entries), nil
}

// runProxy implements the "proxy" subcommand, a faithful port of yx-tools'
// proxy (优选反代) flow: pull IP:port candidates from an external result CSV
// or shared list, then re-test that list with the same TCP/HTTP detection the
// main scanner uses. Per-IP ports (PortMapping) and -colo region filtering are
// applied, mirroring yx-tools' PortMapping and -cfcolo behaviour.
func runProxy(args []string) int {
	fs := flag.NewFlagSet("proxy", flag.ExitOnError)
	in := fs.String("i", "result.csv", "来源文件：测速结果 CSV 或每行 IP[:端口] 的列表")
	out := fs.String("o", "ips_ports.txt", "输出的反代列表文件")
	take := fs.Int("take", 0, "从来源取前 N 条，0 表示全部")
	test := fs.Bool("test", false, "生成列表后直接对该列表测速")
	domain := fs.String("domain", DefaultDomain, "目标域名（TLS SNI + HTTP Host）")
	port := fs.Int("port", DefaultPort, "列表中未指定端口的 IP 使用的端口")
	timeout := fs.Duration("timeout", DefaultTimeout, "单 IP 总超时")
	threads := fs.Int("t", DefaultConcurrency, "并发 worker 数")
	count := fs.Int("n", DefaultTop, "输出最佳 IP 数量")
	delayLimit := fs.Int("tl", 0, "平均延迟上限 ms，超过的淘汰（0 不限制）")
	colo := fs.String("colo", "", "期望地区码（coloss），逗号分隔，如 HKG,SIN（自动启用 HTTPing）")
	httping := fs.Bool("http", true, "用 HTTPing 测速（默认开启；指定 -colo 时强制开启）")
	pingTimes := fs.Int("ping-times", DefaultPingTimes, "启用 -http 时每个 IP 的测速请求次数")
	userAgent := fs.String("user-agent", DefaultUserAgent, "请求 User-Agent")
	maxRun := fs.Int("mt", 0, "整轮测速时长上限（秒），0 不限")
	outputDir := fs.String("output-dir", ".", "输出目录")
	fs.Usage = func() { usageProxy(os.Stdout) }
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 2
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(os.Stderr, "error: unexpected positional arguments: %v\n", fs.Args())
		return 2
	}

	entries, err := loadProxySource(*in)
	if err != nil {
		fmt.Fprintf(os.Stderr, "proxy source error: %v\n", err)
		return 2
	}

	listPath := *out
	if !filepath.IsAbs(listPath) {
		listPath = filepath.Join(*outputDir, listPath)
	}
	n, err := writeProxyList(listPath, entries, *take)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: write %s: %v\n", listPath, err)
		return 2
	}
	fmt.Printf("已生成 %s，共 %d 条\n", listPath, n)
	if !*test {
		return 0
	}

	// Specifying -colo requires HTTPing (region codes only exist in headers),
	// matching yx-tools' Normalize().
	useHTTP := *httping || *colo != ""

	scanner := NewScanner(&Config{
		Domain:      *domain,
		Port:        *port,
		Timeout:     Duration(*timeout),
		Concurrency: *threads,
		Top:         *count,
		MaxIPs:      AbsMaxIPs,
		UserAgent:   *userAgent,
		HTTP:        useHTTP,
		PingTimes:   *pingTimes,
	})
	scanner.Colo = *colo
	scanner.PortMapping = make(map[string]int)

	srcs := make([]Source, 0, len(entries))
	for _, e := range entries {
		p := e.Port
		if p <= 0 {
			p = *port
		}
		scanner.PortMapping[e.IP] = p
		srcs = append(srcs, &proxySource{entries: []proxyEntry{e}})
	}
	multi := NewMultiSource(srcs, uint64(len(srcs)))
	total := multi.Count()

	fmt.Printf("开始对反代列表测速：%d 个候选\n", total)
	fmt.Printf("domain:       %s\n", *domain)
	fmt.Printf("httping:      %v\n", useHTTP)
	if *colo != "" {
		fmt.Printf("colo filter:  %s\n", *colo)
	}
	if *delayLimit > 0 {
		fmt.Printf("delay limit:  %dms\n", *delayLimit)
	}
	fmt.Println()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if *maxRun > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(*maxRun)*time.Second)
		defer cancel()
	}

	scanner.OnProgress = func(scanned, total uint64) {
		if total > 0 {
			fmt.Printf("\rProgress: %d/%d", scanned, total)
		}
	}

	results := scanner.Scan(ctx, multi, *threads, total)
	fmt.Println()

	if *delayLimit > 0 {
		limit := time.Duration(*delayLimit) * time.Millisecond
		filtered := results[:0]
		for _, r := range results {
			if time.Duration(r.Delay) <= limit {
				filtered = append(filtered, r)
			}
		}
		results = filtered
	}

	if len(results) == 0 {
		fmt.Println("no valid IPs found")
		return 1
	}
	results = SortResults(results)

	fmt.Printf("final valid IPs: %d\n", len(results))
	fmt.Println("Top results:")
	for i := 0; i < topLimit(*count, results); i++ {
		r := results[i]
		fmt.Printf("  %-21s status=%-3d delay=%-8s score=%d\n",
			fmt.Sprintf("%s:%d", r.IP, r.Port), r.Status, r.Delay, r.Score)
	}

	bestPath := filepath.Join(*outputDir, "best_ip.txt")
	jsonPath := filepath.Join(*outputDir, "result.json")

	nBest, err := WriteBestIPPorts(bestPath, results, *count)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: write %s: %v\n", bestPath, err)
		return 2
	}
	nJSON, err := WriteJSON(jsonPath, results, *count)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: write %s: %v\n", jsonPath, err)
		return 2
	}

	fmt.Println()
	fmt.Printf("output files: %s (%d IPs), %s (%d results)\n", bestPath, nBest, jsonPath, nJSON)
	return 0
}

// usageProxy prints help for the proxy subcommand.
func usageProxy(out io.Writer) {
	fmt.Fprintf(out, `cfip proxy - 从外部 CSV/列表提取反代 IP:端口 并测速

用法（对齐 yx-tools 的 proxy 优选反代流程）：
  cfip proxy -i result.csv -o ips_ports.txt          # 仅提取列表
  cfip proxy -i result.csv -test                     # 提取后立即测速
  cfip proxy -i list.txt -test -colo HKG,SIN         # 只留回源香港/新加坡

选项：
  -i string        来源文件：测速结果 CSV 或每行 IP[:端口] 的列表
                   （默认 "result.csv"；支持带表头的 ip/port 列或纯列表）
  -o string        输出的反代列表文件（默认 "ips_ports.txt"）
  -take int        从来源取前 N 条，0 表示全部
  -test            生成列表后直接对该列表测速
  -domain string   目标域名，用于 TLS SNI 与 HTTP Host（默认 "ipv4.svi.cc.cd"）
  -port int        列表中未指定端口的 IP 使用的端口（默认 443）
  -timeout duration 单 IP 总超时（默认 300ms）
  -t int           并发 worker 数（默认 500）
  -n int           输出最佳 IP 数量（默认 30）
  -tl int          平均延迟上限 ms，超过的淘汰（0 不限制）
  -colo string     期望地区码，逗号分隔，如 HKG,SIN（指定后自动启用 HTTPing）
  -http            用 HTTPing 测速（默认 true）
  -ping-times int  启用 -http 时每个 IP 的测速请求次数（默认 1）
  -user-agent string 请求 User-Agent
  -mt int          整轮测速时长上限（秒），0 不限
  -output-dir string 输出目录（默认当前目录）
  -h, -help        显示本帮助并退出
`)
}
