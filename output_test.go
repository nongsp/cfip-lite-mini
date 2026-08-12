package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func makeResults() []Result {
	return []Result{
		{IP: "43.198.5.200", Status: 200, Delay: Duration(80 * time.Millisecond), Score: 100},
		{IP: "43.198.5.166", Status: 200, Delay: Duration(38 * time.Millisecond), Score: 100},
		{IP: "43.198.5.100", Status: 403, Delay: Duration(38 * time.Millisecond), Score: 90},
		{IP: "43.198.5.50", Status: 301, Delay: Duration(20 * time.Millisecond), Score: 70},
	}
}

func TestSortResults(t *testing.T) {
	r := makeResults()
	SortResults(r)

	wantOrder := []string{"43.198.5.50", "43.198.5.166", "43.198.5.100", "43.198.5.200"}
	for i, want := range wantOrder {
		if r[i].IP != want {
			t.Fatalf("position %d = %s, want %s (full order: %v)", i, r[i].IP, want, r)
		}
	}
}

func TestSortResultsDelayTieBrokenByStatus(t *testing.T) {
	r := []Result{
		{IP: "b", Status: 403, Delay: Duration(50 * time.Millisecond)},
		{IP: "a", Status: 200, Delay: Duration(50 * time.Millisecond)},
	}
	SortResults(r)
	// same delay: status ascending, so 200 first
	if r[0].IP != "a" || r[0].Status != 200 {
		t.Fatalf("first result = %+v, want status 200 first", r[0])
	}
}

func TestTopLimit(t *testing.T) {
	r := makeResults()
	if n := topLimit(2, r); n != 2 {
		t.Errorf("topLimit(2) = %d", n)
	}
	if n := topLimit(100, r); n != len(r) {
		t.Errorf("topLimit(100) = %d, want %d", n, len(r))
	}
	if n := topLimit(-1, r); n != 0 {
		t.Errorf("topLimit(-1) = %d, want 0", n)
	}
}

func TestWriteBestIP(t *testing.T) {
	path := filepath.Join(t.TempDir(), "best_ip.txt")
	r := makeResults()
	SortResults(r)

	n, err := WriteBestIP(path, r, 2)
	if err != nil {
		t.Fatalf("WriteBestIP: %v", err)
	}
	if n != 2 {
		t.Fatalf("wrote %d IPs, want 2", n)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	want := "43.198.5.50\n43.198.5.166\n"
	if string(data) != want {
		t.Errorf("best_ip.txt = %q, want %q", data, want)
	}
}

func TestWriteJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "result.json")
	r := makeResults()
	SortResults(r)

	n, err := WriteJSON(path, r, 30)
	if err != nil {
		t.Fatalf("WriteJSON: %v", err)
	}
	if n != len(r) {
		t.Fatalf("wrote %d results, want %d", n, len(r))
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	var parsed []struct {
		IP     string `json:"ip"`
		Status int    `json:"status"`
		Delay  string `json:"delay"`
		Score  int    `json:"score"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("result.json is not valid JSON: %v", err)
	}
	if len(parsed) != len(r) {
		t.Fatalf("parsed %d results, want %d", len(parsed), len(r))
	}
	if parsed[0].IP != "43.198.5.50" {
		t.Errorf("first IP = %s", parsed[0].IP)
	}
	if parsed[0].Delay != "20ms" {
		t.Errorf("delay = %q, want 20ms", parsed[0].Delay)
	}
	if parsed[0].Score != 70 {
		t.Errorf("score = %d, want 70", parsed[0].Score)
	}
}

func TestWriteJSONEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "result.json")
	n, err := WriteJSON(path, nil, 30)
	if err != nil {
		t.Fatalf("WriteJSON: %v", err)
	}
	if n != 0 {
		t.Fatalf("wrote %d, want 0", n)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(data) != "[]\n" {
		t.Errorf("empty result.json = %q", data)
	}
}

func TestValidStatusAndScore(t *testing.T) {
	for code, want := range map[int]bool{200: true, 301: true, 302: true, 403: true, 404: false, 500: false, 0: false} {
		if validStatus(code) != want {
			t.Errorf("validStatus(%d) = %v, want %v", code, !want, want)
		}
	}
	if scoreFor(200) != 100 || scoreFor(403) != 90 || scoreFor(301) != 70 || scoreFor(302) != 70 || scoreFor(404) != 0 {
		t.Errorf("scoreFor mismatch")
	}
}
