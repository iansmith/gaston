package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

// TestMuslBucketFixture: the bucket harness compiles a tiny fixture tree —
// one good file, one missing-include failure, one parse failure — and
// reports totals and distinct error buckets.
func TestMuslBucketFixture(t *testing.T) {
	rep := muslCompileBaseline("testdata/muslbucket_fixture", nil, nil)
	if rep.Total != 3 {
		t.Errorf("Total = %d, want 3", rep.Total)
	}
	if rep.Passed != 1 {
		t.Errorf("Passed = %d, want 1", rep.Passed)
	}
	if len(rep.Bucket) < 2 {
		t.Errorf("distinct buckets = %d, want >= 2 (missing-include and parse-error classes)", len(rep.Bucket))
	}
	failed := 0
	for _, files := range rep.Bucket {
		failed += len(files)
	}
	if failed != 2 {
		t.Errorf("bucketed failures = %d, want 2", failed)
	}
}

// TestMuslCompileBaseline is the M1 dashboard runner: opt-in via
// MUSL_BASELINE=1 with the vendored musl/ tree present. It never fails —
// it prints the ranked bucket report (the progress metric for M2/M6).
func TestMuslCompileBaseline(t *testing.T) {
	if os.Getenv("MUSL_BASELINE") == "" {
		t.Skip("set MUSL_BASELINE=1 to run the musl baseline sweep")
	}
	if _, err := os.Stat("musl/src"); err != nil {
		t.Skip("vendored musl/ tree not present")
	}
	includes := []string{
		"musl/arch/aarch64",
		"musl/arch/generic",
		"musl/src/include",
		"musl/src/internal",
		"third-party/musl/overlay/generated",
		"musl/include",
	}
	defines := []string{"_XOPEN_SOURCE=700"}
	rep := muslCompileBaseline("musl/src", includes, defines)

	type row struct {
		class string
		files []string
	}
	rows := make([]row, 0, len(rep.Bucket))
	for c, fs := range rep.Bucket {
		rows = append(rows, row{c, fs})
	}
	sort.Slice(rows, func(i, j int) bool { return len(rows[i].files) > len(rows[j].files) })

	fmt.Printf("\n=== musl compile baseline: %d/%d pass (%d failing, %d buckets) ===\n",
		rep.Passed, rep.Total, rep.Total-rep.Passed, len(rows))
	for _, r := range rows {
		fmt.Printf("%5d  %s\n", len(r.files), r.class)
		for i, f := range r.files {
			if i >= 3 {
				fmt.Printf("        ... and %d more\n", len(r.files)-3)
				break
			}
			fmt.Printf("        %s\n", filepath.Base(f))
		}
	}
}
