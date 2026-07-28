package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
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

// TestMuslReaddirSyscallDispatch is a standing regression test (always runs,
// not gated by MUSL_BASELINE) for the __SYSCALL_DISP ##-paste rescan fix.
// musl/src/dirent/readdir.c calls the variadic __syscall(...) macro, which
// expands through __SYSCALL_DISP -> __SYSCALL_CONCAT -> __SYSCALL_CONCAT_X's
// "a##b" paste into "__syscall3", immediately followed by the argument list
// supplied by __SYSCALL_DISP's own body. Before the preprocessor rescan fix,
// gaston left "__syscall3(...)" unexpanded, so its arguments (e.g. a raw
// pointer dir->buf) reached semcheck without the __scc() "(long)" cast that
// __syscall3's real body applies to every argument, and semcheck correctly
// (but confusingly) rejected the call as "pointer passed where non-pointer
// expected". With the rescan fix, __syscall3(...) itself expands and the
// casts appear, and the file compiles end-to-end.
func TestMuslReaddirSyscallDispatch(t *testing.T) {
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
	src := "musl/src/dirent/readdir.c"
	if _, err := os.Stat(src); err != nil {
		t.Skip("readdir.c not present")
	}
	if class := compileOneForBucket(src, includes, nil); class != "" {
		t.Errorf("readdir.c failed to compile: %s", class)
	}

	// Also confirm, at the preprocessor level, that the __scc() casts are
	// present around the raw dir->buf pointer argument (the specific defect
	// this fix addresses), rather than relying solely on semcheck's silence.
	raw, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	pp := newPreprocessor(includes, nil)
	out, err := pp.Preprocess(string(raw), src)
	if err != nil {
		t.Fatalf("preprocess error: %v", err)
	}
	// Note: __syscall3's own macro body is self-referential
	// ("__syscall3(n,__scc(a),__scc(b),__scc(c))"), so "__syscall3(" is
	// expected to remain in the output verbatim — the fix is that its
	// *arguments* now go through the __scc() "(long)" cast, rather than the
	// whole call site being left completely unexpanded (dropping the casts).
	if !strings.Contains(out, "(long) (dir->buf)") {
		t.Errorf("expected __scc() cast '(long) (dir->buf)' in preprocessed output, got:\n%s", out)
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
