package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"
)

// version is injected at build time via -ldflags "-X main.version=vX.Y.Z".
var version = "dev"

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	opts, err := parseCLI(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		fmt.Fprintln(os.Stderr, "run 'cfip-lite-mini -h' for usage")
		return 2
	}
	if opts.version {
		fmt.Printf("cfip-lite-mini %s\n", version)
		return 0
	}

	cfg := DefaultConfig()
	if fi, statErr := os.Stat(opts.config); statErr == nil && fi.Mode().IsRegular() {
		cfg, err = LoadConfigFile(opts.config)
		if err != nil {
			fmt.Fprintf(os.Stderr, "config error: %v\n", err)
			return 2
		}
	} else if opts.config != "config.yaml" {
		fmt.Fprintf(os.Stderr, "config error: cannot read %s: %v\n", opts.config, statErr)
		return 2
	}

	cfg = opts.merge(cfg)
	if err := cfg.Validate(); err != nil {
		fmt.Fprintf(os.Stderr, "config error: %v\n", err)
		return 2
	}
	if len(cfg.IPRange) == 0 {
		fmt.Fprintln(os.Stderr, "error: no IP/CIDR/range provided (use -cidr, -ip, -range or config.yaml ip_range)")
		return 2
	}

	var sources []Source
	for _, raw := range cfg.IPRange {
		src, err := ParseSource(raw)
		if err != nil {
			fmt.Fprintf(os.Stderr, "ip source error: %v\n", err)
			return 2
		}
		sources = append(sources, src)
	}
	multi := NewMultiSource(sources, uint64(cfg.MaxIPs))
	total := multi.Count()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	scanner := NewScanner(cfg)
	scanner.OnProgress = func(scanned, total uint64) {
		if total > 0 {
			fmt.Printf("\rProgress: %d/%d", scanned, total)
		}
	}

	fmt.Println("=== cfip-lite-mini ===")
	fmt.Printf("domain:       %s\n", cfg.Domain)
	fmt.Printf("port:         %d\n", cfg.Port)
	fmt.Printf("input ranges: %v\n", cfg.IPRange)
	fmt.Printf("total IPs:    %d\n", total)
	fmt.Printf("concurrency:  %d\n", cfg.Concurrency)
	fmt.Printf("timeout:      %s\n", cfg.Timeout)
	fmt.Println()

	scanStart := time.Now()
	results := scanner.Scan(ctx, multi, cfg.Concurrency, total)

	fmt.Println()
	if len(results) == 0 {
		fmt.Println("no valid IPs found")
		return 1
	}
	results = SortResults(results)

	fmt.Printf("final valid IPs: %d\n", len(results))
	fmt.Println("Top results:")
	for i := 0; i < topLimit(cfg.Top, results); i++ {
		r := results[i]
		fmt.Printf("  %-15s status=%-3d delay=%-8s score=%d\n", r.IP, r.Status, r.Delay, r.Score)
	}

	outDir := opts.outputDir
	bestPath := filepath.Join(outDir, "best_ip.txt")
	jsonPath := filepath.Join(outDir, "result.json")

	nTxt, err := WriteBestIP(bestPath, results, cfg.Top)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: write %s: %v\n", bestPath, err)
		return 2
	}
	nJSON, err := WriteJSON(jsonPath, results, cfg.Top)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: write %s: %v\n", jsonPath, err)
		return 2
	}

	fmt.Println()
	fmt.Printf("output files: %s (%d IPs), %s (%d results)\n", bestPath, nTxt, jsonPath, nJSON)
	fmt.Printf("scan finished in %s\n", time.Since(scanStart).Round(time.Microsecond))
	return 0
}
