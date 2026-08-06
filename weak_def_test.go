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

// --- Adversary round-1 gap tests (GAST-8) ---

// TestWeakFuncExternRef: gap finding 1. A cross-TU call to a weak FUNCTION
// definition — the symVA .text case sits behind the same STB_GLOBAL filter,
// so a data-only fix would leave this branching to VA 0.
func TestWeakFuncExternRef(t *testing.T) {
	got := runTentativeProg(t, "weak_func", "weak_func_a", "weak_func_b")
	want := "7\n"
	if got != want {
		t.Errorf("output mismatch:\n  got  %q\n  want %q", got, want)
	}
}

// TestWeakFuncVsStrong: gap finding 1 (precedence pin — green pre-fix). A
// weak function def and a strong function def of the same name: strong wins,
// no duplicate-definition error.
func TestWeakFuncVsStrong(t *testing.T) {
	got := runTentativeProg(t, "weak_funcstrong", "weak_funcstrong_a", "weak_funcstrong_b")
	want := "2\n"
	if got != want {
		t.Errorf("output mismatch:\n  got  %q\n  want %q", got, want)
	}
}

// TestWeakVsCommonReversed: gap finding 2 (precedence pin — green pre-fix).
// Same as TestWeakVsCommon with the objects swapped: in this order the
// COMMON is processed FIRST, so a wrong "last def overwrites the slot" fix
// would let the weak def win. COMMON must win in both orders.
func TestWeakVsCommonReversed(t *testing.T) {
	got := runTentativeProg(t, "weak_common_rev", "weak_common_b", "weak_common_a")
	want := "0\n"
	if got != want {
		t.Errorf("output mismatch:\n  got  %q\n  want %q", got, want)
	}
}

// TestWeakDefNotSupersededByArchive: gap finding 3. A weak def in an
// already-linked user object satisfies references (gABI) — the archive
// member holding a strong def of the same name must NOT be auto-pulled.
func TestWeakDefNotSupersededByArchive(t *testing.T) {
	arPath := buildTentativeArchive(t, "weak_arsup_lib", "weak_arsup_lib")
	got := runTentativeProgWithLibs(t, "weak_arsup",
		[]string{"weak_arsup_a", "weak_arsup_main"}, arPath)
	want := "5\n"
	if got != want {
		t.Errorf("output mismatch:\n  got  %q\n  want %q", got, want)
	}
}

// TestWeakVsCommonSizeMismatch: gap finding 4 (precedence pin — green
// pre-fix). The weak def is LARGER (long, 8B) than the winning COMMON (int,
// 4B); the discarded weak def's size must not corrupt COMMON slot sizing or
// a neighboring COMMON symbol. The guard is read BEFORE main writes it
// (round-2 finding 3): the pre-write 0 is the actual corruption detector.
func TestWeakVsCommonSizeMismatch(t *testing.T) {
	got := runTentativeProg(t, "weak_common_big", "weak_common_big_a", "weak_common_big_b")
	want := "0\n0\n9\n"
	if got != want {
		t.Errorf("output mismatch:\n  got  %q\n  want %q", got, want)
	}
}

// --- Adversary round-2 gap tests (GAST-8) ---

// TestWeakVsStrongLoserSelfRef: round-2 finding 1. The TU whose weak def
// loses also READS it. fileSymVA resolves same-TU references section-locally
// with no precedence check, so a fix that only extends symVA leaves A's
// internal read seeing its own dead copy — the canonical "library ships weak
// default, user's strong def overrides internal references" case.
func TestWeakVsStrongLoserSelfRef(t *testing.T) {
	got := runTentativeProg(t, "weak_selfref", "weak_selfref_a", "weak_selfref_b")
	want := "2\n"
	if got != want {
		t.Errorf("output mismatch:\n  got  %q\n  want %q", got, want)
	}
}

// TestWeakUninitExternRef: round-2 finding 2. An UNINITIALIZED weak def
// takes objgen's third emission path (STB_WEAK in .bss, not .data and not
// COMMON). Cross-TU ref must see zeroed storage, not crash at VA 0.
func TestWeakUninitExternRef(t *testing.T) {
	got := runTentativeProg(t, "weak_uninit", "weak_uninit_a", "weak_uninit_b")
	want := "0\n"
	if got != want {
		t.Errorf("output mismatch:\n  got  %q\n  want %q", got, want)
	}
}

// --- Adversary round-3 gap tests (GAST-8) ---

// TestWeakUninitSharedStorage: round-3 finding 1. Weak .bss storage
// IDENTITY: the uninitialized weak def is one shared slot, so a write from
// TU B must be visible through TU A's read. A per-TU-duplicated zeroed slot
// passes TestWeakUninitExternRef but fails here.
func TestWeakUninitSharedStorage(t *testing.T) {
	got := runTentativeProg(t, "weak_uninitshare", "weak_uninitshare_a", "weak_uninitshare_b")
	want := "6\n"
	if got != want {
		t.Errorf("output mismatch:\n  got  %q\n  want %q", got, want)
	}
}

// TestWeakVsCommonLoserSelfRef: round-3 finding 2. COMMON beats weak, and
// the losing TU reads its own weak def — the same fileSymVA interposition
// mechanism as TestWeakVsStrongLoserSelfRef, for the COMMON-winner case. A
// strong-only interposition fix leaves A reading its dead .data copy (5).
func TestWeakVsCommonLoserSelfRef(t *testing.T) {
	got := runTentativeProg(t, "weak_commonself", "weak_commonself_a", "weak_commonself_b")
	want := "0\n"
	if got != want {
		t.Errorf("output mismatch:\n  got  %q\n  want %q", got, want)
	}
}

// TestWeakFuncVsStrongReversed: round-3 finding 3 (precedence pin — green
// pre-fix). Function strong-vs-weak in the opposite link order (strong
// first), mirroring the data family's Reversed pin: kills per-section
// last-def-wins fixes.
func TestWeakFuncVsStrongReversed(t *testing.T) {
	got := runTentativeProg(t, "weak_funcstrong_rev", "weak_funcstrong_b", "weak_funcstrong_a")
	want := "2\n"
	if got != want {
		t.Errorf("output mismatch:\n  got  %q\n  want %q", got, want)
	}
}

// --- Adversary round-4 gap test (GAST-8) ---

// TestWeakVsWeakLoserSelfRef: round-4 finding 1, the final discriminating
// case. Weak-vs-weak is the precedence TIE, so "redirect a self-reference
// iff a HIGHER-precedence def exists elsewhere" (wrong) and "all references
// resolve to the single chosen definition" (gABI, right) diverge only here:
// B's self-read must see A's winning first-in-link-order def.
func TestWeakVsWeakLoserSelfRef(t *testing.T) {
	got := runTentativeProg(t, "weak_wwself", "weak_wwself_a", "weak_wwself_b")
	want := "11\n"
	if got != want {
		t.Errorf("output mismatch:\n  got  %q\n  want %q", got, want)
	}
}
