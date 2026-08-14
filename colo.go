package main

import (
	"net/http"
	"regexp"
	"strings"
)

// Colo code (airport/region code) extraction and filtering, ported from
// yx-tools' proxy mode. A colo code is only available from HTTP response
// headers, so colo filtering only takes effect in HTTPing mode.

var (
	regexpColoIATACode    = regexp.MustCompile(`[A-Z]{3}`)  // IATA airport code, e.g. SJC
	regexpColoCountryCode = regexp.MustCompile(`[A-Z]{2}`)  // country code, e.g. US
	regexpColoGcore       = regexp.MustCompile(`^[a-z]{2}`) // lowercase city code, e.g. fr
)

// getHeaderColo extracts a colo/region code from CDN response headers.
// Supported CDNs mirror yx-tools: Cloudflare (cf-ray), CDN77 (x-77-pop),
// BunnyCDN (server), AWS CloudFront (x-amz-cf-pop), Fastly (x-served-by) and
// Gcore (x-id-fe). Returns "" when no supported header is present.
func getHeaderColo(header http.Header) (colo string) {
	if header.Get("server") != "" {
		// Cloudflare: server: cloudflare, cf-ray: 7bd32409eda7b020-SJC
		if header.Get("server") == "cloudflare" {
			if colo = header.Get("cf-ray"); colo != "" {
				return regexpColoIATACode.FindString(colo)
			}
		}
		// CDN77: server: CDN77-Turbo, x-77-pop: losangelesUSCA
		if header.Get("server") == "CDN77-Turbo" {
			if colo = header.Get("x-77-pop"); colo != "" {
				return regexpColoCountryCode.FindString(colo)
			}
		}
		// BunnyCDN: server: BunnyCDN-TW1-1121
		if colo = header.Get("server"); strings.Contains(colo, "BunnyCDN-") {
			return regexpColoCountryCode.FindString(strings.TrimPrefix(colo, "BunnyCDN-"))
		}
	}
	// AWS CloudFront: x-amz-cf-pop: SIN52-P1
	if colo = header.Get("x-amz-cf-pop"); colo != "" {
		return regexpColoIATACode.FindString(colo)
	}
	// Fastly: x-served-by: cache-fra-etou8220141-FRA (last entry is the
	// serving pop)
	if colo = header.Get("x-served-by"); colo != "" {
		if matches := regexpColoIATACode.FindAllString(colo, -1); len(matches) > 0 {
			return matches[len(matches)-1]
		}
	}
	// Gcore: x-id-fe: fr5-hw-edge-gc17
	if colo = header.Get("x-id-fe"); colo != "" {
		if colo = regexpColoGcore.FindString(colo); colo != "" {
			return strings.ToUpper(colo)
		}
	}
	return ""
}

// splitColos splits a comma separated colo list into upper case codes,
// dropping empty entries.
func splitColos(s string) []string {
	parts := strings.Split(strings.ToUpper(s), ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// matchColo reports whether the extracted colo is in the wanted list. An
// empty want list matches everything.
func matchColo(want []string, got string) bool {
	if len(want) == 0 {
		return true
	}
	for _, w := range want {
		if w == got {
			return true
		}
	}
	return false
}
