# Gaston Musl Libc Support — Phased Implementation Plan

**Goal:** Compile musl's ~1,494 generic `.c` files with gaston as the C compiler,
using musl's container-based configure to generate headers/config, and linking the
result as a static library (initially). No subprocess launching; everything encoded
natively in gaston.

**Branch strategy:** One commit per logical step (per project policy).

---

## Dependency Graph

```
Phase 1 (long double) ──────────────────────────────────────────────┐
Phase 2 (STV_HIDDEN) ──────────────────────────────────────────────┤
Phase 3 (file-scope asm)                                             ├──▶ Phase 7 (musl build)
Phase 4 (inline asm infra) ──▶ Phase 5 (missing encoders) ─────────┤
Phase 6 (_Complex guard) ──────────────────────────────────────────┘
```

Phases 1–3 and 6 are independent of each other.  
Phase 5 depends on Phase 4 (needs the asm dispatch infrastructure to wire up
the new encoders).  
Phase 7 depends on all prior phases.

---

## Phase 1 — `long double` as 16-byte quad type

**Why first:** `max_align_t` in musl's `stddef.h` is defined as the largest
fundamental alignment, which on AArch64 is 16 bytes (quad-precision float).
If `sizeof(long double) == 8` (current: aliased to `double`), every malloc block
header will be mis-aligned, breaking the heap.

### 1.1 — Add `TypeLongDouble` to the type system

**File:** `ast.go`

- Add `TypeLongDouble` to the `TypeKind` enum after `TypeDouble`.
- In the size/alignment tables (wherever `TypeDouble` returns 8/8):
  - `sizeof(long double)` → `16`
  - `alignof(long double)` → `16`
- In all switch statements that dispatch on `TypeKind`, add a
  `TypeLongDouble` case that falls through to the `TypeDouble` path for
  arithmetic (the value is stored in a 128-bit slot but we use the low
  64 bits for now; true quad arithmetic is out of scope).

**File:** `semcheck.go`

- When `long double` is parsed from a declaration specifier sequence,
  resolve it to `TypeLongDouble` rather than `TypeDouble`.

**File:** `preproc.go`

- Remove or update the `#define LDBL_MANT_DIG 53` and `_LDBL_EQ_DBL 1`
  defines that alias long double to double. Replace with:
  ```c
  #define LDBL_MANT_DIG    113   // IEEE 754 binary128
  #define LDBL_MAX_EXP     16384
  #define LDBL_MIN_EXP     (-16381)
  ```
  (These values match the AArch64 ABI / GCC values musl's headers expect.)

**File:** `arm64.go` / `irgen.go`

- Stack slot allocation: `TypeLongDouble` variables get a 16-byte-aligned,
  16-byte slot. The high 64 bits are zero-initialized (we only use the low
  64 bits for arithmetic until true quad ops are added).
- Load/store: emit `LDR Q<n>` / `STR Q<n>` (128-bit SIMD load/store) for
  `TypeLongDouble` locals. These encodings already exist in the SIMD
  extension; add `encLDRQ` / `encSTRQ` to `arm64enc.go` if not present.

**Test:** Add `testdata/longdouble_size.c` asserting
`sizeof(long double) == 16` and `_Alignof(long double) == 16`.

---

## Phase 2 — ELF symbol visibility (`STV_HIDDEN`)

**Why:** Musl marks ~171 internal symbols `__attribute__((visibility("hidden")))`.
For a correct `.so` these must set `STV_HIDDEN` in the ELF symbol table; without
it, all internals are visible, causing symbol conflicts when musl is dynamically
linked. For static linking this matters less, but it is cheap to implement and
the wrong behavior will silently break `.so` builds later.

### 2.1 — `st_other` field in objgen.go

**File:** `objgen.go`

ELF `Elf64_Sym.st_other` is currently always written as zero (`STV_DEFAULT`).
Change the symbol-table emitter to accept a `visibility` byte per symbol:
- `STV_DEFAULT  = 0`
- `STV_HIDDEN   = 2`
- `STV_PROTECTED = 3`

The `st_other` byte in Elf64_Sym encodes visibility in bits [1:0].

**File:** `ir.go` / `ast.go`

Add a `Visibility` field to `IRGlobal` and `IRFunc` (or to `Symbol` in the
semantic table). Default is `STV_DEFAULT`.

### 2.2 — Wire up `__attribute__((visibility("hidden")))`

**File:** `semcheck.go` (or wherever `__attribute__` is parsed/applied)

When the attribute parser sees `visibility("hidden")`, set `Visibility =
STV_HIDDEN` on the current declaration's symbol. Similarly for `"protected"`.

**Test:** `testdata/hidden_sym.c` — compile, inspect `.symtab` with `readelf -s`,
verify `st_other == 2` for the tagged symbol.

---

## Phase 3 — File-scope `__asm__` (verbatim string emission)

**Why:** `crt_arch.h` uses bare file-scope `__asm__("...")` to inject startup
assembly. This is the simplest inline-asm form (no operands, no constraints) and
is a prerequisite for crt startup.

### 3.1 — Parse file-scope `__asm__` into an AST node

**File:** `grammar.y`

Change the current grammar rule:
```yacc
| ASM_KW '(' args ')' ';'  { $$ = nil }   // currently discards
```
to:
```yacc
| ASM_KW '(' STRING_LITERAL ')' ';'  { $$ = newAsmStmt($3, nil, nil, nil) }
```
(Separate the file-scope and block-scope forms; file-scope has no volatile,
no output/input/clobber lists.)

**File:** `ast.go`

Add `KindAsmStmt` to `NodeKind`. The AST node holds:
- `Template string` — the raw asm text
- `Outputs, Inputs []*AsmOperand` — nil for file-scope
- `Clobbers []string` — nil for file-scope
- `IsVolatile bool`

### 3.2 — IR representation

**File:** `ir.go`

Add `IROpAsmVerbatim` opcode. The IR instruction carries the raw template string.
No operand resolution at this phase (file-scope form has none).

### 3.3 — Codegen: emit template verbatim into `.text`

**File:** `arm64.go`

For `IROpAsmVerbatim`: write the template string byte-for-byte into the current
function's (or a synthetic init function's) code buffer. Because the template is
raw assembly text — not machine bytes — the **output format** at this phase is
`.s` text, not binary `.o`. See the integration note in Phase 4.

> **Alternative (simpler):** For file-scope asm, reserve a dedicated
> `__asm_verbatim_N` section in the `.o` and place the bytes there.
> Requires the assembler to have pre-encoded the bytes. Since gaston
> doesn't invoke an external assembler, emit a "raw bytes" section using
> a `SHT_PROGBITS` section with the pre-encoded instruction bytes.
> For `crt_arch.h` the instructions are fixed (`.text` section start
> boilerplate); encode them by hand.

**Practical path for Phase 3:** Treat file-scope `__asm__` blocks as
a list of pre-encoded literal instruction words. Gaston recognizes the
specific strings that appear in musl's `crt_arch.h` and emits their
AArch64 machine words directly. This is less general but avoids needing
a full text-to-binary assembler in Phase 3.

**Test:** Compile a minimal `.c` with a file-scope `__asm__` block containing
a known instruction; disassemble the resulting `.o` and verify the instruction
is present.

---

## Phase 4 — Inline asm infrastructure (the critical phase)

**Why:** This is the largest and most complex work item. Musl uses inline asm for
syscalls, atomics, TLS access, and barriers. Without it, any musl function that
makes a syscall (e.g., `write`, `read`, `mmap`) will silently omit the syscall
and return garbage.

The complete constraint set from musl AArch64 is (as confirmed):
- Output: `"=r"`, `"=&r"`, `"=w"`, `"=Q"`
- Input:  `"r"`, `"w"`, `"Q"`, `"0"`, `"1"` (match constraints)
- Modifiers: `"+w"` (read-write)
- Clobbers: `"memory"`, `"cc"`

Operand references in the template: `%N`, `%wN`, `%xN`, `%dN`, `%sN`.

### 4.1 — Lexer: scan the full `__asm__` form

**File:** `lexer.go`

Replace `skipAsmBody()` with a real scanner that returns a new `ASM_STMT` token
(or multiple tokens). The full syntax to scan:
```
__asm__ volatile? ( STRING_LITERAL
    (: asm_operand_list?
    (: asm_operand_list?
    (: clobber_list?)?)?)? )
```
Where `asm_operand_list` is `[STRING_LITERAL] ( expr )` items separated by
commas, and `clobber_list` is `STRING_LITERAL` items separated by commas.

The lexer should NOT skip this; it should tokenize it so the parser can build
the AST.

**File:** `grammar.y`

Add production rules:
```yacc
asm_stmt:
    ASM_KW asm_volatile_opt '(' STRING_LITERAL ')' ';'
  | ASM_KW asm_volatile_opt '(' STRING_LITERAL ':' asm_operand_list ')' ';'
  | ASM_KW asm_volatile_opt '(' STRING_LITERAL ':' asm_operand_list ':' asm_operand_list ')' ';'
  | ASM_KW asm_volatile_opt '(' STRING_LITERAL ':' asm_operand_list ':' asm_operand_list ':' clobber_list ')' ';'
  ;
```

### 4.2 — Named register variables

**File:** `grammar.y`

Extend the variable declarator grammar to recognize:
```c
register long x8 __asm__("x8") = n;
```
The `__asm__("regname")` suffix on a local variable declaration pins the variable
to a physical register. Grammar addition:
```yacc
declarator: ... ASM_KW '(' STRING_LITERAL ')'   { $$.RegName = $3 }
```

**File:** `semcheck.go`

When a variable has `.RegName` set, record it in the symbol entry.
The register allocator must not assign a different register to this variable.

**File:** `irgen.go`

Introduce `IROpPinReg` or annotate `IRAddr` with a `PinnedReg` field.
When generating code for a pinned variable, skip the normal slot allocation
and use the fixed register directly.

**File:** `arm64.go`

In the register allocator, treat pinned registers as pre-allocated (remove them
from the free pool at function entry, restore at function exit if callee-saved).

### 4.3 — Constraint resolution

**File:** `irgen.go` or new `asmgen.go`

For each `__asm__` statement:
1. Assign a physical register (or memory address) to each operand based on its
   constraint string:
   - `"r"` / `"=r"` / `"=&r"` → allocate a GPR (`x0`–`x18`, avoiding pinned
     regs and the current ABI scratch regs in use)
   - `"w"` / `"=w"` / `"+w"` → allocate an FP/SIMD register (`d0`–`d7`)
   - `"Q"` / `"=Q"` → address operand; the C expression must be an lvalue;
     emit its address in a GPR and format as `[xN]` in the template
   - `"0"` / `"1"` → match constraint: same register as output operand 0 or 1
2. For output operands: after the asm block, emit a store from the assigned
   register back to the C lvalue (unless the operand is `=&r` early clobber,
   in which case the store is still needed but no input sharing is allowed).
3. For input operands: before the asm block, emit a load from the C expression
   into the assigned register.
4. For `"memory"` clobber: emit a barrier fence in the IR (or force a register
   spill / reload around the asm block).

### 4.4 — Template operand substitution

**File:** `irgen.go` or `asmgen.go`

Walk the template string, replacing:
- `%N`  → GPR name for operand N (e.g., `x3`)
- `%wN` → 32-bit alias of GPR N (e.g., `w3`)
- `%xN` → 64-bit GPR (same as `%N` for GPRs)
- `%dN` → 64-bit FP register (e.g., `d2`)
- `%sN` → 32-bit FP register (e.g., `s2`)
- `%%`  → literal `%`

After substitution, the template is a fully-specified AArch64 assembly string
like `"mrs x3, tpidr_el0"`.

### 4.5 — Text-to-binary assembly dispatch

**File:** `asmgen.go` (new file)

Given a substituted instruction string, call the appropriate `encXxx` function.
Parse instruction mnemonic and operands from the string.

This is an **instruction dispatcher**, not a general assembler. It handles
exactly the ~13 instructions musl uses (see Phase 5 for the encoders).
Unknown instructions cause a compile error (`unsupported inline asm instruction: "foo"`).

The dispatcher:
1. Split on whitespace to get the mnemonic.
2. Strip a trailing `.` condition code if present (not used by musl).
3. Switch on mnemonic → call the corresponding `encXxx` with parsed register
   operands.

**Test suite for Phase 4:**
- `testdata/asm_basic.c` — simple `__asm__("nop")` no-op
- `testdata/asm_output.c` — `__asm__("mov %0, #42" : "=r"(x))`, verify `x == 42`
- `testdata/asm_input.c` — `__asm__("add %0, %1, %2" : "=r"(c) : "r"(a), "r"(b))`
- `testdata/asm_pinned_reg.c` — `register long x8 __asm__("x8") = 5;`
- `testdata/asm_memory_clobber.c` — verify spill/reload around memory clobber
- `testdata/asm_match_constraint.c` — `"0"` match constraint

---

## Phase 5 — Missing AArch64 instruction encoders

**Why here:** These are needed by the inline asm dispatcher (Phase 4.5). They are
also the mechanically simplest part of the work — each function is ~5 lines of
bit manipulation, directly derivable from the ARMv8 Architecture Reference Manual
(ARM DDI 0487).

All new functions go in `arm64enc.go`.

### 5.1 — System register access

```
mrs Xt, tpidr_el0   →  encoding: 1101 0101 0011 0011 1101 0000 0<Rt>
msr tpidr_el0, Xt   →  encoding: 1101 0101 0001 0011 1101 0000 0<Rt>
```

Add `encMRS_tpidr_el0(rt uint32) uint32` and `encMSR_tpidr_el0(rt uint32) uint32`.

These are fixed bit patterns except for the 5-bit `Rt` field. The system register
field is hardcoded to `TPIDR_EL0` (op0=3, op1=3, CRn=13, CRm=0, op2=2,
encoded as `S3_3_C13_C0_2`).

### 5.2 — Data memory barrier

```
dmb ish   →  fixed encoding: 0xD5033BBF
```

`ish` = option field `0b1011`. This is a constant; add:
```go
func encDMBish() uint32 { return 0xD5033BBF }
```

### 5.3 — Load/store exclusive (for atomics)

```
ldaxr Wt, [Xn]         →  LDAXR 32-bit acquire
stlxr Ws, Wt, [Xn]    →  STLXR 32-bit release
```

ARMv8 encoding (C6.2.133, C6.2.252):
- `ldaxr Wt,[Xn]`: `1000 1000 0101 1111 1111 11<Rn><Rt>` (size=10, L=1, o0=1)
- `stlxr Ws,Wt,[Xn]`: `1000 1000 000<Rs> 1111 11<Rn><Rt>` (size=10, L=0, o0=1)

```go
func encLDAXRW(rt, rn uint32) uint32
func encSTLXRW(rs, rt, rn uint32) uint32
```

### 5.4 — FP square root

```
fsqrt Dd, Dn   →  encoding: 0x1E61C000 | (dn<<5) | dd
```

`FSQRT` (scalar double): `0001 1110 0110 0001 1100 00<Rn><Rd>`, `ftype=01` (double).

```go
func encFSQRTD(rd, rn uint32) uint32
```

### 5.5 — FP multiply-add

```
fmadd Dd, Dn, Dm, Da   →  encoding: 0x1F400000 | (dm<<16) | (da<<10) | (dn<<5) | dd
```

`FMADD` (scalar double): `0001 1111 01<Rm>0<Ra><Rn><Rd>`.

```go
func encFMADDD(rd, rn, rm, ra uint32) uint32
```

### 5.6 — FP rounding (frintm / frintp / frinta / frintx)

All share the encoding `0x1E644000 | (rmode<<23) | (rn<<5) | rd` (roughly).

`FRINT` (scalar double): `0001 1110 0110 01<rmode>1000 00<Rn><Rd>`

| Mnemonic | rmode | Description |
|----------|-------|-------------|
| `frintp` | `001` | Round toward +∞ |
| `frintm` | `010` | Round toward −∞ |
| `frinta` | `100` | Round to nearest, ties away |
| `frintx` | `110` | Round to nearest, exact |
| `frintz` | `011` | Round toward zero |
| `frintn` | `000` | Round to nearest, ties to even |

```go
func encFRINTD(rd, rn, rmode uint32) uint32
```

Then define named wrappers:
```go
func encFRINTMD(rd, rn uint32) uint32 { return encFRINTD(rd, rn, 0b010) }
func encFRINTPD(rd, rn uint32) uint32 { return encFRINTD(rd, rn, 0b001) }
func encFRINTAD(rd, rn uint32) uint32 { return encFRINTD(rd, rn, 0b100) }
func encFRINTXD(rd, rn uint32) uint32 { return encFRINTD(rd, rn, 0b110) }
```

### 5.7 — FP max/min number (fmaxnm / fminnm)

```
fmaxnm Dd, Dn, Dm   →  FMAXNM scalar double
fminnm Dd, Dn, Dm   →  FMINNM scalar double
```

`FMAXNM`: `0001 1110 0110 1<Rm>0110 00<Rn><Rd>` → base `0x1E606800`  
`FMINNM`: `0001 1110 0110 1<Rm>0111 00<Rn><Rd>` → base `0x1E607800`

```go
func encFMAXNMD(rd, rn, rm uint32) uint32
func encFMINNMD(rd, rn, rm uint32) uint32
```

**Test each encoder** with a unit test in `testdata/` that executes the instruction
and checks the result. Use the existing `asm_basic.c` pattern.

---

## Phase 6 — `_Complex` graceful suppression

**Current state:** `__STDC_NO_COMPLEX__ = 1` is defined in `preproc.go`. Musl's
own `complex.h` checks this and skips complex declarations. However, if the
`_Complex` keyword itself appears in a translation unit (e.g., as a `typedef` in a
header that doesn't gate on the macro), the parser will hit an unknown token and
panic.

### 6.1 — Lexer: recognize `_Complex` as a no-op qualifier

**File:** `lexer.go`

Add `_Complex` and `__complex__` to the keyword table, returning a new token
`COMPLEX_KW`. (Or just return `IGNORED_KW` and discard.)

**File:** `grammar.y`

In the type-specifier rules, add:
```yacc
type_specifier: ... | COMPLEX_KW { $$ = nil /* treated as double */ }
```

When combined with a floating type (`double _Complex`), treat the result as
`TypeDouble` (the real part; imaginary is dropped). Emit a warning, not an error.

This is sufficient for musl's usage, which limits `_Complex` to its own internal
`complex.h` (which is already guarded by `__STDC_NO_COMPLEX__`).

**Test:** Compile `double _Complex z;` and verify no panic (may warn).

---

## Phase 7 — Musl build integration

**Prerequisites:** Phases 1–6 complete.

### 7.1 — Container-based configure

Run musl's `./configure` inside a Docker container (standard musl build container
or a minimal Alpine image) with:
```sh
./configure --target=aarch64-linux-musl --disable-shared CC=gcc
```
This generates:
- `config.mak` — build variables
- `include/bits/alltypes.h` — platform-specific typedefs
- `include/bits/syscall.h` — syscall numbers
- `obj/include/` — computed headers

The generated headers are committed to `third-party/musl/include/` in the repo
(similar to how Lua headers are handled).

### 7.2 — Gaston compile script

Write `third-party/musl/build.sh` that:
1. Exports `CC=/path/to/gaston` and `CFLAGS=-c -I.../musl/include ...`
2. Iterates over the ~1,494 `.c` files in `src/` (excluding `src/arch/` files
   that are not `aarch64` and excluding files that need assembly processing)
3. Compiles each with gaston: `$CC $CFLAGS -o obj/$file.o src/$file.c`
4. Archives the `.o` files: `gaston -ar libmusl.a obj/*.o`

### 7.3 — Incremental gap fixing

Run the build script and fix compilation errors as they surface. Expected
remaining gaps (based on the gap analysis):

| Likely gap | Fix location | Expected effort |
|------------|-------------|----------------|
| `__attribute__((section(...)))` on functions | `semcheck.go` | Low — already no-op the attribute, add section routing |
| `__attribute__((alias("sym")))` | `semcheck.go`, `objgen.go` | Medium — emit alias symbol |
| Variadic FP args (`__builtin_va_arg` with `double`) | `irgen.go` | Low — AArch64 AAPCS64 variadic FP uses GPRs too |
| `_Atomic` qualifier | `lexer.go`, `grammar.y` | Low — parse as no-op qualifier |
| `__builtin_expect(expr, val)` | `irgen.go` | Trivial — return `expr` unchanged |
| Computed goto (`goto *ptr`) | `grammar.y`, `irgen.go`, `arm64.go` | High — needed for some optimized paths |

### 7.4 — Linking test

Once `libmusl.a` is buildable, write a minimal test program that links against it
and calls `write(1, "hello\n", 6)` via musl. This exercises:
- musl's syscall inline asm (Phase 4+5)
- musl's TLS setup (Phase 5.1, MRS tpidr_el0)
- musl's startup code (Phase 3, file-scope asm)
- Correct `long double` alignment (Phase 1)

Verify with `qemu-aarch64-static ./test_musl` on a Linux host or directly on
an AArch64 target.

---

## Implementation Order and Commit Plan

| Step | Phase | Files changed | Commit message |
|------|-------|--------------|----------------|
| 1 | 1.1 | `ast.go`, `semcheck.go`, `irgen.go` | `types: add TypeLongDouble (16-byte, AArch64 quad)` |
| 2 | 1.1 | `arm64enc.go`, `arm64.go` | `arm64: add encLDRQ/encSTRQ for 128-bit slot loads` |
| 3 | 1.1 | `preproc.go` | `preproc: update LDBL macros for binary128 (not alias-to-double)` |
| 4 | 2.1 | `objgen.go`, `ir.go` | `elf: add st_other/STV_HIDDEN visibility field to symbols` |
| 5 | 2.2 | `semcheck.go` | `sem: wire __attribute__((visibility("hidden"))) to STV_HIDDEN` |
| 6 | 6.1 | `lexer.go`, `grammar.y` | `parse: recognize _Complex as no-op qualifier (no panic)` |
| 7 | 3.1 | `grammar.y`, `ast.go` | `parse: file-scope __asm__ into AsmStmt node (not discarded)` |
| 8 | 3.2–3.3 | `ir.go`, `arm64.go` | `codegen: emit file-scope asm template verbatim into .text` |
| 9 | 4.1 | `lexer.go`, `grammar.y` | `parse: full inline asm syntax (outputs:inputs:clobbers)` |
| 10 | 4.2 | `grammar.y`, `semcheck.go`, `irgen.go` | `asm: named register variables (register T x __asm__("xN"))` |
| 11 | 4.3 | `irgen.go` / `asmgen.go` | `asm: constraint resolution and register assignment` |
| 12 | 4.4 | `asmgen.go` | `asm: operand substitution (%N, %wN, %xN, %dN, %sN)` |
| 13 | 4.5 | `asmgen.go` | `asm: text-to-binary dispatcher for inline asm instructions` |
| 14 | 5.1 | `arm64enc.go` | `arm64enc: encMRS/encMSR tpidr_el0` |
| 15 | 5.2 | `arm64enc.go` | `arm64enc: encDMBish (data memory barrier)` |
| 16 | 5.3 | `arm64enc.go` | `arm64enc: encLDAXRW/encSTLXRW (load/store exclusive)` |
| 17 | 5.4 | `arm64enc.go` | `arm64enc: encFSQRTD (FP square root double)` |
| 18 | 5.5 | `arm64enc.go` | `arm64enc: encFMADDD (FP multiply-add double)` |
| 19 | 5.6 | `arm64enc.go` | `arm64enc: encFRINTD and frintm/frintp/frinta/frintx wrappers` |
| 20 | 5.7 | `arm64enc.go` | `arm64enc: encFMAXNMD/encFMINNMD (FP max/min number)` |
| 21 | 7.1 | `third-party/musl/` | `musl: add configure output (headers, bits/, config.mak)` |
| 22 | 7.2 | `third-party/musl/build.sh` | `musl: add gaston build script for ~1,494 .c files` |
| 23+ | 7.3 | various | `musl: fix <specific gap>` (one commit per gap fixed) |

---

## Risk Register

| Risk | Likelihood | Mitigation |
|------|-----------|------------|
| Inline asm register allocator conflicts with normal allocation | High | Allocate asm regs from a scratch pool; mark them "in use" for the asm block's lifetime |
| musl's startup asm (crt_arch.h) uses instructions not in the dispatcher | Medium | Read crt_arch.h before Phase 3 and pre-encode all needed instructions |
| `long double` in variadics breaks AAPCS64 calling convention | Medium | AArch64 passes `long double` as two GPRs in AAPCS64; audit irgen's variadic path |
| musl uses GCC statement expressions `({ ... })` internally | Low | Not present in musl's generic code (only in headers, which gaston sees preprocessed) |
| musl's Makefile invokes tools gaston can't replicate | Low | Container configure handles this; build.sh only invokes `gaston -c` per file |
| Some of the 1,494 files use unsupported C11/C99 features | Medium | Compile incrementally; fix per-file |

---

## Out of Scope (for this plan)

- **`_Complex` arithmetic** — `__STDC_NO_COMPLEX__` suppresses musl's complex.h.
  True complex arithmetic is not needed for musl's core 1,494 files.
- **Shared library (`.so`) output** — Static linking only for initial integration.
  STV_HIDDEN (Phase 2) lays the groundwork but the linker does not yet emit
  `PT_DYNAMIC` / `DT_*` entries.
- **Dynamic TLS (`__tls_get_addr`)** — Musl's static TLS is accessed via
  `mrs tpidr_el0` (Phase 5.1). Full dynamic TLS with `__tls_get_addr` trampolines
  is out of scope.
- **Thread cancellation** — `pthread_cancel` requires unwinding support; not needed
  for the initial build target.
- **True quad-precision arithmetic** — Phase 1 gives `long double` the correct size
  and alignment; actual quad-precision `__float128` operations are not wired up.
  Code that performs arithmetic on `long double` values will silently use the
  double-precision path (sufficient for musl, which uses `long double` mainly for
  `max_align_t`).

---

## Addendum — Additional Incremental Gap Fixes

The following gaps are expected to surface during Phase 7.3 and are tracked here
for completeness. Each is a separate commit.

- **`_Atomic` no-op qualifier** — musl uses `_Atomic` on a handful of global
  variables (e.g., in `src/thread/`). On single-core bare-metal this is a no-op;
  add `_Atomic` and `__atomic` to the lexer keyword table and treat as a discarded
  type qualifier in `grammar.y`, exactly like `volatile`.

- **`__builtin_expect(expr, val)` → `expr`** — used pervasively in musl for branch
  prediction hints. Add to the builtin dispatch in `irgen.go`: evaluate and return
  the first argument unchanged; ignore the second. No IR opcode needed.

- **`__attribute__((alias("sym")))` symbol alias** — musl uses this to make a public
  name an alias for an internal implementation (e.g., `weak_alias` macro in
  `src/internal/libc.h`). Requires two changes: (1) `semcheck.go` records the alias
  target on the symbol; (2) `objgen.go` emits a second `Elf64_Sym` entry with the
  same `st_value` and section index as the target symbol, binding
  `STB_GLOBAL`/`STB_WEAK` as appropriate. The `weak_alias` macro in musl expands
  to `__attribute__((weak, alias("target")))`, so both `weak` and `alias`
  attributes must be handled together.
