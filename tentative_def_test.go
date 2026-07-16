package main

import (
	"fmt"
	"os"
	"os/exec"
	"testing"
)

// Phase 0 red tests for GAST-3: C tentative definitions (file-scope objects
// without initializers) must receive real BSS storage at link time. gaston's
// objgen emits them as ELF COMMON symbols; the linker must allocate them —
// today it treats COMMON as undefined and patches every reference to VA 0,
// so any program touching a tentative-defined global (including libgastonc's
// own stdin/stdout/stderr) crashes at startup.

// runTentativeProg compiles the given testdata sources, links them against
// libgastonc.a, runs the binary in an ARM64 Alpine container, and returns
// stdout.
func runTentativeProg(t *testing.T, name string, srcs ...string) string {
	t.Helper()
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker not found in PATH; skipping container tests")
	}
	libPath := buildLibgastonc(t)

	var objs []string
	for _, src := range srcs {
		obj := fmt.Sprintf("/tmp/gaston-test-%s-%s.o", name, src)
		objs = append(objs, obj)
		t.Cleanup(func() { os.Remove(obj) })
		if err := compileObjPath("testdata/"+src+".c", obj, nil); err != nil {
			t.Fatalf("compile %s: %v", src, err)
		}
	}

	binPath := "/tmp/gaston-test-" + name
	t.Cleanup(func() { os.Remove(binPath) })
	if err := link(binPath, append(objs, libPath)); err != nil {
		t.Fatalf("link: %v", err)
	}

	cmd := exec.Command("docker", "run", "--rm",
		"--platform", "linux/arm64",
		"-i",
		"-v", binPath+":/prog",
		"alpine:latest",
		"/prog",
	)
	out, err := cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			t.Fatalf("docker run failed (exit %d):\nstdout: %s\nstderr: %s",
				ee.ExitCode(), string(out), string(ee.Stderr))
		}
		t.Fatalf("docker run: %v", err)
	}
	return string(out)
}

// TestTentativeDefs: single-TU tentative definitions — scalar, pointer, array,
// and 16-byte-aligned long double — get zeroed, correctly aligned storage.
func TestTentativeDefs(t *testing.T) {
	got := runTentativeProg(t, "tentative_def", "tentative_def")
	want := "5\n7\n0\n2\n3\n0\n"
	if got != want {
		t.Errorf("output mismatch:\n  got  %q\n  want %q", got, want)
	}
}

// TestTentativeMultiTU: the same symbol tentatively defined in two TUs merges
// into one storage slot, and a real (initialized) definition in one TU wins
// over a tentative definition of the same symbol in another.
func TestTentativeMultiTU(t *testing.T) {
	got := runTentativeProg(t, "tentative_multi", "tentative_multi_a", "tentative_multi_b")
	want := "42\n9\n"
	if got != want {
		t.Errorf("output mismatch:\n  got  %q\n  want %q", got, want)
	}
}
