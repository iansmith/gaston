package main

import (
	"testing"
)

// Phase 0 red tests for GAST-8: STB_WEAK definitions never enter symVA.
//
// The symVA build loop (linker.go ~591), the archive-pull scan (linker.go
// ~383/392), the COMMON/duplicate merge loop (linker.go ~500-538), and the
// archive symbol index (arfmt.go ~83 archiveCreate, ~215 archiveRead) all
// filter on `sym.binding == elf.STB_GLOBAL`, so a weak definition
// (`__attribute__((weak)) int flag = 1;`) is invisible everywhere the
// linker looks for a definition. A cross-TU extern reference to a weak-only
// def resolves to VA 0 (crash / garbage), a weak def loses to a COMMON of
// the same name only by accident of the COMMON code path (not proper
// precedence), and archive members whose only symbol is a weak def are
// never lazily pulled.
//
// These tests reuse the GAST-3 tentative-definition test harness
// (runTentativeProg / runTentativeProgWithLibs / buildTentativeArchive in
// tentative_def_test.go and tentative_adversary_test.go) since weak defs
// share the same cross-TU link-and-run shape.

// TestWeakExternRef: DoD 1. A cross-TU extern reference to a weak def must
// resolve to the def's VA — runtime-correct value, not 0/crash.
func TestWeakExternRef(t *testing.T) {
	got := runTentativeProg(t, "weak_extern", "weak_extern_a", "weak_extern_b")
	want := "7\n"
	if got != want {
		t.Errorf("output mismatch:\n  got  %q\n  want %q", got, want)
	}
}

// TestWeakVsStrong: DoD 2. A strong (STB_GLOBAL) def and a weak def of the
// same name must link with NO duplicate-definition error, and the strong
// definition must win.
func TestWeakVsStrong(t *testing.T) {
	got := runTentativeProg(t, "weak_strong", "weak_strong_a", "weak_strong_b")
	want := "2\n"
	if got != want {
		t.Errorf("output mismatch:\n  got  %q\n  want %q", got, want)
	}
}

// TestWeakVsStrongReversed: same as TestWeakVsStrong but with the strong
// definition's object linked first — precedence must not depend on load
// order.
func TestWeakVsStrongReversed(t *testing.T) {
	got := runTentativeProg(t, "weak_strong_rev", "weak_strong_b", "weak_strong_a")
	want := "2\n"
	if got != want {
		t.Errorf("output mismatch:\n  got  %q\n  want %q", got, want)
	}
}

// TestWeakVsCommon: DoD 3. A weak def and a COMMON (tentative) def of the
// same name: per ELF gABI, "the link editor honors the common definition
// and ignores the weak ones." Precedence order is strong > COMMON > weak.
func TestWeakVsCommon(t *testing.T) {
	got := runTentativeProg(t, "weak_common", "weak_common_a", "weak_common_b")
	want := "0\n"
	if got != want {
		t.Errorf("output mismatch:\n  got  %q\n  want %q", got, want)
	}
}

// TestWeakVsWeakFirstWins: DoD 4. Two weak defs, no strong def anywhere: no
// error, and the first weak def in link order wins.
func TestWeakVsWeakFirstWins(t *testing.T) {
	got := runTentativeProg(t, "weak_ww_ab", "weak_ww_a", "weak_ww_b", "weak_ww_main")
	want := "11\n"
	if got != want {
		t.Errorf("output mismatch:\n  got  %q\n  want %q", got, want)
	}
}

// TestWeakVsWeakFirstWinsReversed: same as TestWeakVsWeakFirstWins but with
// the two weak-defining objects swapped — proves the winner tracks link
// order, not symbol name or file identity.
func TestWeakVsWeakFirstWinsReversed(t *testing.T) {
	got := runTentativeProg(t, "weak_ww_ba", "weak_ww_b", "weak_ww_a", "weak_ww_main")
	want := "22\n"
	if got != want {
		t.Errorf("output mismatch:\n  got  %q\n  want %q", got, want)
	}
}

// TestWeakOnlyInArchiveMember: DoD 5. The sole definition of g_weak is a
// weak def inside an archive member that exports no other referenced
// symbol. The archive symbol index must list weak-bound symbols, or the
// member is never pulled and the extern reference resolves to nothing.
func TestWeakOnlyInArchiveMember(t *testing.T) {
	arPath := buildTentativeArchive(t, "weak_armem_lib", "weak_armem")
	got := runTentativeProgWithLibs(t, "weak_armem", []string{"weak_armem_main"}, arPath)
	want := "3\n"
	if got != want {
		t.Errorf("output mismatch:\n  got  %q\n  want %q", got, want)
	}
}
