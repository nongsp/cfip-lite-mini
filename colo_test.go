package main

import (
	"net/http"
	"testing"
)

func TestGetHeaderColoCloudflare(t *testing.T) {
	h := http.Header{}
	h.Set("Server", "cloudflare")
	h.Set("CF-Ray", "7bd32409eda7b020-SJC")
	if got := getHeaderColo(h); got != "SJC" {
		t.Errorf("cloudflare colo = %q, want SJC", got)
	}
}

func TestGetHeaderColoCloudFront(t *testing.T) {
	h := http.Header{}
	h.Set("X-Amz-Cf-Pop", "SIN52-P1")
	if got := getHeaderColo(h); got != "SIN" {
		t.Errorf("cloudfront colo = %q, want SIN", got)
	}
}

func TestGetHeaderColoFastly(t *testing.T) {
	h := http.Header{}
	h.Set("X-Served-By", "cache-qpg1275-QPG")
	if got := getHeaderColo(h); got != "QPG" {
		t.Errorf("fastly colo = %q, want QPG", got)
	}
	// multiple entries: last one is the serving pop
	h2 := http.Header{}
	h2.Set("X-Served-By", "cache-fra-etou8220141-FRA, cache-hhr-khhr2060043-HHR")
	if got := getHeaderColo(h2); got != "HHR" {
		t.Errorf("fastly multi colo = %q, want HHR", got)
	}
}

func TestGetHeaderColoBunny(t *testing.T) {
	h := http.Header{}
	h.Set("Server", "BunnyCDN-TW1-1121")
	if got := getHeaderColo(h); got != "TW" {
		t.Errorf("bunny colo = %q, want TW", got)
	}
}

func TestGetHeaderColoCDN77(t *testing.T) {
	h := http.Header{}
	h.Set("Server", "CDN77-Turbo")
	h.Set("X-77-Pop", "frankfurtDE")
	if got := getHeaderColo(h); got != "DE" {
		t.Errorf("cdn77 colo = %q, want DE", got)
	}
}

func TestGetHeaderColoGcore(t *testing.T) {
	h := http.Header{}
	h.Set("X-Id-Fe", "fr5-hw-edge-gc17")
	if got := getHeaderColo(h); got != "FR" {
		t.Errorf("gcore colo = %q, want FR", got)
	}
}

func TestGetHeaderColoEmpty(t *testing.T) {
	if got := getHeaderColo(http.Header{}); got != "" {
		t.Errorf("empty header colo = %q, want empty", got)
	}
}

func TestSplitColos(t *testing.T) {
	got := splitColos("hkg, SIN , ")
	want := []string{"HKG", "SIN"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("splitColos = %v, want %v", got, want)
	}
	if len(splitColos("")) != 0 {
		t.Errorf("splitColos(\"\") should be empty")
	}
}

func TestMatchColo(t *testing.T) {
	if !matchColo(nil, "SIN") {
		t.Error("empty want list should match everything")
	}
	if !matchColo([]string{"HKG", "SIN"}, "SIN") {
		t.Error("SIN should match [HKG SIN]")
	}
	if matchColo([]string{"HKG", "SIN"}, "LAX") {
		t.Error("LAX should not match [HKG SIN]")
	}
}

func TestScannerRejectsColo(t *testing.T) {
	s := &Scanner{Colo: "HKG,SIN"}
	if s.rejectsColo("LAX") == false {
		t.Error("LAX should be rejected by [HKG,SIN]")
	}
	if s.rejectsColo("SIN") {
		t.Error("SIN should pass [HKG,SIN]")
	}
	if s.rejectsColo("") == false {
		t.Error("empty colo should be rejected when filter is set")
	}
}
