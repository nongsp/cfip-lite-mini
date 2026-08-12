package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
)

// SortResults orders results by delay ascending, then status, then IP.
func SortResults(results []Result) []Result {
	sort.SliceStable(results, func(i, j int) bool {
		if results[i].Delay != results[j].Delay {
			return results[i].Delay < results[j].Delay
		}
		if results[i].Status != results[j].Status {
			return results[i].Status < results[j].Status
		}
		return results[i].IP < results[j].IP
	})
	return results
}

// topLimit returns the number of results to write for a given top value.
func topLimit(top int, results []Result) int {
	if top < 0 {
		top = 0
	}
	if top > len(results) {
		top = len(results)
	}
	return top
}

// WriteBestIP writes one IP per line to path, keeping only the top results.
// It returns the number of IPs written.
func WriteBestIP(path string, results []Result, top int) (int, error) {
	f, err := os.Create(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	n := topLimit(top, results)
	for i := 0; i < n; i++ {
		if _, err := fmt.Fprintln(f, results[i].IP); err != nil {
			return i, err
		}
	}
	return n, nil
}

// WriteJSON writes the top results as a JSON array to path.
// It returns the number of results written.
func WriteJSON(path string, results []Result, top int) (int, error) {
	n := topLimit(top, results)
	var data []byte
	var err error
	if n == 0 {
		data = []byte("[]")
	} else {
		data, err = json.MarshalIndent(results[:n], "", "  ")
		if err != nil {
			return 0, err
		}
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return 0, err
	}
	return n, nil
}
