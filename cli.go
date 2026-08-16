package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

// repeatableFlag collects multiple occurrences of the same flag into a slice.
type repeatableFlag struct {
	values []string
}

func (r *repeatableFlag) String() string { return strings.Join(r.values, ",") }

func (r *repeatableFlag) Set(v string) error {
	r.values = append(r.values, v)
	return nil
}

// CLIOptions holds raw command line values plus markers for which flags
// were explicitly set, so CLI > config.yaml > defaults precedence works.
type CLIOptions struct {
	domain      string
	cidr        repeatableFlag
	ip          repeatableFlag
	rng         repeatableFlag
	port        int
	timeout     time.Duration
	concurrency int
	top         int
	maxIPs      int
	userAgent   string
	config      string
	outputDir   string
	http        bool
	pingTimes   int
	version     bool

	portSet        bool
	timeoutSet     bool
	concurrencySet bool
	topSet         bool
	maxIPsSet      bool
	userAgentSet   bool
	httpSet        bool
	pingTimesSet   bool
}

func newFlagSet(opts *CLIOptions, out io.Writer) *flag.FlagSet {
	fs := flag.NewFlagSet("cfip", flag.ContinueOnError)
	fs.SetOutput(out)
	fs.Usage = func() { usage(os.Stdout) }

	fs.StringVar(&opts.domain, "domain", "", "target domain used for TLS SNI and HTTP Host (default \"ipv4.svi.cc.cd\")")
	fs.Var(&opts.cidr, "cidr", "CIDR block to scan, repeatable (e.g. 43.198.0.0/16)")
	fs.Var(&opts.ip, "ip", "single IP to scan, repeatable (e.g. 43.198.5.166)")
	fs.Var(&opts.rng, "range", "IP range start-end, repeatable (e.g. 159.60.146.10-159.60.146.200)")
	fs.IntVar(&opts.port, "port", 0, "HTTPS port (default 443)")
	fs.DurationVar(&opts.timeout, "timeout", 0, "per-IP total timeout (default 300ms)")
	fs.IntVar(&opts.concurrency, "concurrency", 0, "number of concurrent workers (default 500)")
	fs.IntVar(&opts.top, "top", 0, "number of best IPs to write to output (default 30)")
	fs.IntVar(&opts.maxIPs, "max-ips", 0, "maximum IPs to scan (default 1000000, hard cap 10000000)")
	fs.StringVar(&opts.userAgent, "user-agent", "", "User-Agent header sent to targets")
	fs.StringVar(&opts.config, "config", "config.yaml", "path to the YAML config file")
	fs.StringVar(&opts.outputDir, "output-dir", ".", "directory where best_ip.txt and result.json are written")
	fs.BoolVar(&opts.http, "http", true, "use yx-tools style HTTPing (per-IP transport, RST close, averaged multi-request latency); default on, disable with -http=false")
	fs.IntVar(&opts.pingTimes, "ping-times", 0, "requests per IP when -http is enabled (default 1)")
	fs.BoolVar(&opts.version, "version", false, "print version and exit")

	return fs
}

// parseCLI parses command line arguments and records which flags were set.
func parseCLI(args []string) (*CLIOptions, error) {
	opts := &CLIOptions{}
	fs := newFlagSet(opts, io.Discard)
	if err := fs.Parse(args); err != nil {
		return nil, err
	}
	if fs.NArg() > 0 {
		return nil, fmt.Errorf("unexpected positional arguments: %v", fs.Args())
	}
	fs.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "port":
			opts.portSet = true
		case "timeout":
			opts.timeoutSet = true
		case "concurrency":
			opts.concurrencySet = true
		case "top":
			opts.topSet = true
		case "max-ips":
			opts.maxIPsSet = true
		case "user-agent":
			opts.userAgentSet = true
		case "http":
			opts.httpSet = true
		case "ping-times":
			opts.pingTimesSet = true
		}
	})
	return opts, nil
}

// usage prints help to the given writer.
func usage(out io.Writer) {
	fmt.Fprintf(out, `cfip - a minimal high-concurrency CIDR/IP HTTPS availability scanner

Usage:
  cfip [options]
  cfip proxy [options]   # 优选反代：从 CSV/列表提取 IP:端口 并测速

Options:
  -domain string       target domain used for TLS SNI and HTTP Host
                       (default "ipv4.svi.cc.cd")
  -cidr string         CIDR block to scan, repeatable (e.g. 43.198.0.0/16)
  -ip string           single IP to scan, repeatable (e.g. 43.198.5.166)
  -range string        IP range start-end, repeatable (e.g. 159.60.146.10-159.60.146.200)
  -port int            HTTPS port (default 443)
  -timeout duration    per-IP total timeout (default 300ms)
  -concurrency int     number of concurrent workers (default 500)
  -top int             number of best IPs to write to output (default 30)
  -max-ips int         maximum IPs to scan (default 1000000, hard cap 10000000)
  -user-agent string   User-Agent header sent to targets
  -config string       path to the YAML config file (default "config.yaml")
  -output-dir string   directory for output files (default ".")
  -http                use yx-tools style HTTPing: per-IP transport with
                       RST close, redirects stopped, latency averaged over
                       -ping-times requests (default true, disable with
                       -http=false)
  -ping-times int      requests per IP when -http is enabled (default 1)
  -version             print version and exit
  -h, -help            show this help and exit

Run 'cfip proxy -h' for proxy subcommand options.

CIDR/IP/Range flags may be repeated and override ip_range from config.yaml.
`)
}

// merge applies CLI overrides on top of a (possibly file loaded) config.
func (o *CLIOptions) merge(cfg *Config) *Config {
	if o.domain != "" {
		cfg.Domain = o.domain
	}
	ranges := make([]string, 0, len(o.cidr.values)+len(o.ip.values)+len(o.rng.values))
	ranges = append(ranges, o.cidr.values...)
	ranges = append(ranges, o.ip.values...)
	ranges = append(ranges, o.rng.values...)
	if len(ranges) > 0 {
		cfg.IPRange = ranges
	}
	if o.portSet {
		cfg.Port = o.port
	}
	if o.timeoutSet {
		cfg.Timeout = Duration(o.timeout)
	}
	if o.concurrencySet {
		cfg.Concurrency = o.concurrency
	}
	if o.topSet {
		cfg.Top = o.top
	}
	if o.maxIPsSet {
		cfg.MaxIPs = o.maxIPs
	}
	if o.userAgentSet {
		cfg.UserAgent = o.userAgent
	}
	if o.httpSet {
		cfg.HTTP = o.http
	}
	if o.pingTimesSet {
		cfg.PingTimes = o.pingTimes
	}
	return cfg
}
