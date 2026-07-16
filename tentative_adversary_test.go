package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// Adversary gap tests for GAST-3 Phase 0 (tentative-definition / COMMON
// storage). Each covers a way a plausible-but-wrong implementation of the
// COMMON fix could pass the base Phase 0 suite:
//
//   - link-order reversal (a last-wins symbol map resolves by accident)
//   - size mismatch between tentative defs (max size must win)
//   - pure-extern reference resolved only by a COMMON def
//   - .rela.data ABS64 against a COMMON target (second relocation path)
//   - archive symbol index must list COMMON symbols (lazy pull)
//   - tentative def in a user object must NOT pull an archive member's
//     real definition
//   - COMMON storage vs sbrk/heap extent, large COMMON, alignment,
//     coexistence with static .bss/.data, same-TU redeclaration

// TestTentativeMultiTUReversed: same as TestTentativeMultiTU but with the
// objects linked in the opposite order — the real definition is processed
// BEFORE the tentative one. Catches last-wins symbol-map implementations.
func TestTentativeMultiTUReversed(t *testing.T) {
	got := runTentativeProg(t, "tentative_multi_rev", "tentative_multi_b", "tentative_multi_a")
	want := "42\n9\n"
	if got != want {
		t.Errorf("output mismatch:\n  got  %q\n  want %q", got, want)
	}
}

// TestTentativeSizeMerge: `buf` is long[1] in TU A and long[4] in TU B — the
// merged COMMON must take the maximum size, and neighboring symbols must not
// overlap the full extent.
func TestTentativeSizeMerge(t *testing.T) {
	got := runTentativeProg(t, "tentative_size", "tentative_size_a", "tentative_size_b")
	want := "5\n7\n1\n"
	if got != want {
		t.Errorf("output mismatch:\n  got  %q\n  want %q", got, want)
	}
}

// TestTentativeExternRef: TU B references `tvar` via extern; the only
// definition anywhere is TU A's tentative one. The allocated VA must be
// visible through the global symbol table, not just file-locally.
func TestTentativeExternRef(t *testing.T) {
	got := runTentativeProg(t, "tentative_extern", "tentative_extern_a", "tentative_extern_b")
	want := "5\n"
	if got != want {
		t.Errorf("output mismatch:\n  got  %q\n  want %q", got, want)
	}
}

// TestTentativeDataPtr: an initialized global pointer targets a tentative
// object — exercises the .rela.data ABS64 path against COMMON.
func TestTentativeDataPtr(t *testing.T) {
	got := runTentativeProg(t, "tentative_data_ptr", "tentative_data_ptr")
	want := "6\n1\n"
	if got != want {
		t.Errorf("output mismatch:\n  got  %q\n  want %q", got, want)
	}
}

// TestTentativeHeap: COMMON storage must lie inside the BSS extent used to
// place the sbrk break — malloc'd memory must not overlay tentative globals.
// Also verifies the far end of a 1MB COMMON is real, zeroed storage.
func TestTentativeHeap(t *testing.T) {
	got := runTentativeProg(t, "tentative_heap", "tentative_heap")
	want := "11\n0\n1\n"
	if got != want {
		t.Errorf("output mismatch:\n  got  %q\n  want %q", got, want)
	}
}

// TestTentativeAlign: p(16-align) q(8) r(16-align) — r needs padding under
// both declaration order and name order, so an alignment-ignoring allocator
// fails either way.
func TestTentativeAlign(t *testing.T) {
	got := runTentativeProg(t, "tentative_align", "tentative_align")
	want := "0\n0\n0\n"
	if got != want {
		t.Errorf("output mismatch:\n  got  %q\n  want %q", got, want)
	}
}

// TestTentativeMixed: tentative defs coexist with static .bss and
// initialized .data in one TU without overlap.
func TestTentativeMixed(t *testing.T) {
	got := runTentativeProg(t, "tentative_mixed", "tentative_mixed")
	want := "1\n2\n0\n0\n3\n"
	if got != want {
		t.Errorf("output mismatch:\n  got  %q\n  want %q", got, want)
	}
}

// TestTentativeRedecl: C11 6.9.2p2 within one TU — tentative + real
// definition is one real definition; two tentatives are one tentative.
func TestTentativeRedecl(t *testing.T) {
	got := runTentativeProg(t, "tentative_redecl", "tentative_redecl")
	want := "3\n4\n"
	if got != want {
		t.Errorf("output mismatch:\n  got  %q\n  want %q", got, want)
	}
}

// buildTentativeArchive compiles the given testdata sources and archives
// them into a temporary .a, returning its path.
func buildTentativeArchive(t *testing.T, name string, srcs ...string) string {
	t.Helper()
	var objs []string
	for _, src := range srcs {
		obj := fmt.Sprintf("/tmp/gaston-test-%s-%s.o", name, src)
		objs = append(objs, obj)
		t.Cleanup(func() { os.Remove(obj) })
		if err := compileObjPath("testdata/"+src+".c", obj, nil); err != nil {
			t.Fatalf("compile %s: %v", src, err)
		}
	}
	arPath := fmt.Sprintf("/tmp/gaston-test-%s.a", name)
	t.Cleanup(func() { os.Remove(arPath) })
	if err := archiveCreate(arPath, objs); err != nil {
		t.Fatalf("archive: %v", err)
	}
	return arPath
}

// TestTentativeOnlyInArchiveMember: the sole definition of g_common is a
// tentative def inside an archive member with no other referenced symbols.
// The archive symbol index must list COMMON symbols, or the member is never
// pulled and the reference resolves to nothing.
func TestTentativeOnlyInArchiveMember(t *testing.T) {
	arPath := buildTentativeArchive(t, "tentative_armem_lib", "tentative_armem")
	got := runTentativeProgWithLibs(t, "tentative_armem", []string{"tentative_armem_main"}, arPath)
	want := "3\n"
	if got != want {
		t.Errorf("output mismatch:\n  got  %q\n  want %q", got, want)
	}
}

// TestTentativeUserVsArchiveReal: the user object tentatively defines g_tent;
// an archive member holds a real definition (= 40) but exports nothing else
// referenced. Classic semantics: the user object's tentative def satisfies
// the symbol, the member is NOT pulled, and g_tent is zero-initialized.
func TestTentativeUserVsArchiveReal(t *testing.T) {
	arPath := buildTentativeArchive(t, "tentative_arreal_lib", "tentative_arreal")
	got := runTentativeProgWithLibs(t, "tentative_arreal", []string{"tentative_arreal_main"}, arPath)
	want := "0\n"
	if got != want {
		t.Errorf("output mismatch:\n  got  %q\n  want %q", got, want)
	}
}

// runInAlpine runs a linked binary in the ARM64 Alpine container and returns
// its stdout.
func runInAlpine(t *testing.T, binPath string) string {
	t.Helper()
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

// runTentativeProgWithLibs is runTentativeProg with extra archives inserted
// before libgastonc.a in link order.
func runTentativeProgWithLibs(t *testing.T, name string, srcs []string, extraLibs ...string) string {
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

	inputs := append(objs, extraLibs...)
	inputs = append(inputs, libPath)

	binPath := "/tmp/gaston-test-" + name
	t.Cleanup(func() { os.Remove(binPath) })
	if err := link(binPath, inputs); err != nil {
		t.Fatalf("link: %v", err)
	}
	return runInAlpine(t, binPath)
}

// TestDuplicateStrongDefRejected: two REAL (initialized) definitions of the
// same symbol must be a link-time error, not silent last-wins. (No docker
// needed — asserts on link() itself.)
func TestDuplicateStrongDefRejected(t *testing.T) {
	dir := t.TempDir()
	srcA := dir + "/dup_a.c"
	srcB := dir + "/dup_b.c"
	if err := os.WriteFile(srcA, []byte("long dupv = 1;\nvoid main(void) { output((int)dupv); }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(srcB, []byte("long dupv = 2;\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	objA, objB := dir+"/dup_a.o", dir+"/dup_b.o"
	if err := compileObjPath(srcA, objA, nil); err != nil {
		t.Fatalf("compile dup_a: %v", err)
	}
	if err := compileObjPath(srcB, objB, nil); err != nil {
		t.Fatalf("compile dup_b: %v", err)
	}
	err := link(dir+"/dup", []string{objA, objB, buildLibgastonc(t)})
	if err == nil {
		t.Fatalf("link succeeded; want duplicate-definition error for 'dupv'")
	}
	if !strings.Contains(err.Error(), "dupv") {
		t.Errorf("error does not name the duplicate symbol: %v", err)
	}
}
