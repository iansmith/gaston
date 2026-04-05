# Gaston: Fix `int` to 4 Bytes (C ABI Correctness)

## Problem

Gaston currently treats `int` and `unsigned int` as 8-byte types in struct layout
and `sizeof`. Real C (LP64, which is what ARM64 Linux uses) makes `int` 4 bytes and
`long` 8 bytes. This divergence causes:

- Wrong struct field offsets whenever any field is declared `int`
- Wrong `sizeof(int)` (gaston returns 8, should be 4)
- Silent wrong overflow behaviour (`int` wraps at 2^63, not 2^31)
- Will block compiling real programs like bc, lua, micropython

`float` and `double` are already correct (4 and 8 bytes respectively).

## Current Codebase State

Key files:

- `cmd/gaston/ast.go` — `TypeKind` constants; `fieldSizeAlign()`; `StructField.ByteOffset`;
  `StructDef.SizeBytes()`.
- `cmd/gaston/ir.go` — `IROpCode` list; `IRQuad.TypeHint` (already used by
  `IRFieldLoad`/`IRFieldStore` to carry the field's `TypeKind`); `AddrKind`.
- `cmd/gaston/irgen.go` — `buildStructDefIR()`; all expression code-gen including
  field-access, deref, assignment.
- `cmd/gaston/semcheck.go` — `buildStructDef()` (parallel to irgen's version);
  type-checking of field accesses and assignments; `sizeof` folding.
- `cmd/gaston/arm64.go` — Plan 9 assembly emitter (used for non-Docker targets).
- `cmd/gaston/elfgen.go` — ELF binary emitter (used by `TestDockerRun`/`TestLibc`).
- `cmd/gaston/docker_test.go` — all integration tests.

Relevant existing IR opcodes:

```
IRFieldLoad   // Dst = field (int):    base in Src1, byte-offset in Src2.IVal; TypeHint = field TypeKind
IRFieldStore  // field store (int):   base in Dst,  value in Src1, offset in Src2.IVal; TypeHint = field TypeKind
IRFFieldLoad  // Dst = field (double): same shape
IRFFieldStore // field store (double): same shape
IRDerefLoad   // Dst = *Src1         8-byte pointer-width load
IRDerefStore  // *Dst = Src1         8-byte pointer-width store
IRDerefCharLoad  // 1-byte load via computed ptr (added recently)
IRDerefCharStore // 1-byte store via computed ptr
IRSignExtend  // Dst = sign_extend(Src1, Src2.IVal)  Src2.IVal = bit width (8 or 16)
IRZeroExtend  // Dst = zero_extend(Src1, Src2.IVal)
```

`IRQuad.TypeHint` is already set on `IRFieldLoad`/`IRFieldStore` quads to carry the
field's TypeKind so the code generators can choose the right instruction width.

## Plan

Work in four phases. Each phase is independently testable. Do not start the next
phase until the previous one's tests all pass.

---

### Phase 1 — Struct layout + sizeof (no codegen change yet)

Goal: `sizeof(int) == 4`; struct fields of type `int` get 4-byte offsets/sizes.

1. **`ast.go` — `fieldSizeAlign`**: add explicit cases before the `default`:
   ```go
   case TypeInt:
       return 4, 4
   case TypeUnsignedInt:
       return 4, 4
   ```
   The `default` (8, 8) then covers `TypeDouble`, `TypePtr`, `TypeFuncPtr`, etc.

2. **`semcheck.go` — `buildStructDef`**: that function duplicates the offset
   computation. Verify it calls `fieldSizeAlign` (it does via the shared function in
   ast.go) — no change needed if the call already routes through `fieldSizeAlign`.

3. **`irgen.go` — `buildStructDefIR`**: same — verify it calls `fieldSizeAlign` and
   doesn't hard-code 8 for TypeInt anywhere.

4. **Test**: write `testdata/struct_int_layout.cm` and add to `docker_test.go`:
   ```c
   struct S { int a; int b; };  // should be 8 bytes, a@0 b@4
   struct T { char c; int n; }; // should be 8 bytes (4-align pad after char), c@0 n@4
   struct U { int x; double d; }; // x@0(4 bytes), pad 4, d@8 → size 16
   int main(void) {
       printf("%d\n", (int)sizeof(struct S)); // 8
       printf("%d\n", (int)sizeof(struct T)); // 8
       printf("%d\n", (int)sizeof(struct U)); // 16
       printf("%d\n", (int)sizeof(int));      // 4
       return 0;
   }
   ```
   Expected: `8\n8\n16\n4\n`

After Phase 1, field *offsets* are right but loads/stores still use 8-byte
instructions. That's OK for now — we'll fix the width in Phase 2.

---

### Phase 2 — Field load/store width (the core codegen change)

Goal: `int` struct fields are read/written as 32-bit quantities.

The IR already carries `TypeHint` on `IRFieldLoad`/`IRFieldStore`. Both code
generators need to branch on it.

#### `elfgen.go` — `IRFieldLoad` case

Current code does an unconditional `LDR X` (64-bit load). Change to:

```go
case IRFieldLoad:
    g.load(q.Src1, regX0)          // base pointer → X0
    off := q.Src2.IVal
    switch q.TypeHint {
    case TypeChar:
        g.cb.emit(encLDRSBuoff(regX0, regX0, off))  // sign-extend 8→64
    case TypeUnsignedChar:
        g.cb.emit(encLDRBuoff(regX0, regX0, off))   // zero-extend 8→64
    case TypeShort:
        g.cb.emit(encLDRSHuoff(regX0, regX0, off))  // sign-extend 16→64
    case TypeUnsignedShort:
        g.cb.emit(encLDRHuoff(regX0, regX0, off))   // zero-extend 16→64
    case TypeInt:
        g.cb.emit(encLDRSWuoff(regX0, regX0, off))  // sign-extend 32→64 (LDRSW)
    case TypeUnsignedInt:
        g.cb.emit(encLDRWuoff(regX0, regX0, off))   // zero-extend 32→64 (LDR W)
    case TypeFloat:
        // float fields need FP load — see Phase 2b
        g.cb.emit(encLDRSuoff(regS0, regX0, off))
        g.cb.emit(encFCVT_SD(regD0, regS0))         // widen f32→f64 for internal use
        g.store(regD0, q.Dst)                        // store as double temp
        continue
    default: // TypeDouble, pointers — 8-byte
        g.cb.emit(encLDRuoff(regX0, regX0, off))
    }
    g.store(regX0, q.Dst)
```

Encoding helpers needed (add to elfgen.go's encoding section):
- `encLDRSBuoff(dst, base, off)` — LDRSB (sign-extend byte, 64-bit dest): `0x8B400000 | ...`
- `encLDRSHuoff(dst, base, off)` — LDRSH 64: `0x8B800000 | ...`  
- `encLDRHuoff(dst, base, off)` — LDRH (unsigned 16→64): `0x79400000 | ...`
- `encLDRSWuoff(dst, base, off)` — LDRSW (sign-extend 32→64): `0xB9800000 | (off/4 << 10) | (base << 5) | dst`
- `encLDRWuoff(dst, base, off)` — LDR W (zero-extend 32→64): `0xB9400000 | (off/4 << 10) | (base << 5) | dst`

Similarly for `IRFieldStore`:
- `TypeInt` / `TypeUnsignedInt` → `STR W` (`encSTRWuoff`): `0xB9000000 | ...`
- `TypeChar` / `TypeUnsignedChar` → `STRB` (already exists as `encSTRBuoff`)
- `TypeShort` / `TypeUnsignedShort` → `STRH` (`encSTRHuoff`): `0x79000000 | ...`

#### `arm64.go` — same branching on TypeHint

Use Plan 9 mnemonics:
- `MOVWU (R0), R1` — 32-bit unsigned load (zero-extend)
- `MOVW (R0), R1` — 32-bit signed load (sign-extend) — Plan 9 uses `MOVW` for LDRSW
- `MOVW R1, (R0)` — 32-bit store
- `MOVHU` / `MOVH` — 16-bit
- `MOVBU` / `MOVB` — 8-bit (already used)

**Test**: extend the Phase 1 test program to also read/write the fields and verify
correct values. Add a test that stores 0x100000001 into an `int` field and reads it
back — should get 1 (truncated to 32 bits), not 0x100000001.

---

### Phase 3 — `int*` pointer derefs (for `%d` / `&intvar` patterns)

When `irgen` emits a dereference through an `int*` pointer (e.g. `sscanf`'s `%d`
writes to `int*`), it currently emits `IRDerefLoad`/`IRDerefStore` which are always
8 bytes. These need to be width-aware too.

Two options:

**Option A (minimal):** Add `TypeHint` to `IRDerefLoad`/`IRDerefStore` quads and use
it in the code generators the same way as Phase 2. `irgen` already knows the pointee
type when generating a deref — propagate it to the quad.

**Option B (new opcodes):** Add `IRDerefInt32Load` / `IRDerefInt32Store` analogous
to `IRDerefCharLoad`/`IRDerefCharStore`. More explicit, avoids overloading TypeHint.

Option A is recommended — less churn, consistent with how `IRFieldLoad` works.

Changes:
- `ir.go`: add `TypeHint` documentation for `IRDerefLoad`/`IRDerefStore` (already has the field)
- `irgen.go`: when emitting `IRDerefLoad`/`IRDerefStore` for a TypeInt/TypeUnsignedInt
  pointee, set `TypeHint = TypeInt` / `TypeHint = TypeUnsignedInt`
- `elfgen.go`/`arm64.go`: branch on `TypeHint` in the `IRDerefLoad`/`IRDerefStore` cases

**sscanf impact**: `%d` currently passes `long*` to `va_arg`. Once pointers to `int`
deref correctly, the C programs should use `int x; sscanf(..., "%d", &x)` — but
since gaston's `int` is now 4 bytes and the frame slot for `x` is still 8-byte
aligned, storing 4 bytes into the low 4 bytes of the slot is fine on little-endian
ARM64 (which is what we target). No sscanf.cm change needed for correctness, but the
libc comment saying "all int types are 64-bit" should be updated.

**Test**: `testdata/deref_int32.cm`:
```c
int main(void) {
    int x;
    int* p;
    p = &x;
    *p = 0x7fffffff;
    printf("%d\n", x);  // 2147483647
    *p = *p + 1;
    printf("%d\n", x);  // -2147483648 (if overflow; or 2147483648 if not yet Phase 4)
    return 0;
}
```

---

### Phase 4 — Arithmetic overflow/truncation for `int`-typed variables (optional / deferred)

This is the hardest phase. In real C, arithmetic on `int` wraps at 32 bits. After
`int a = 2147483647; a = a + 1;`, `a` is `-2147483648`.

Currently gaston does all arithmetic in 64-bit. To fix this properly:

- Every `IRAdd`/`IRSub`/`IRMul`/`IRNeg` result that is typed `TypeInt` must be
  followed by a sign-extend-from-32 operation (equivalent to `SXTW` on ARM64 /
  `mov eax, eax; ...` on x86).
- This requires temporaries to carry a `TypeKind`. IRQuad doesn't currently track
  the result type of arithmetic ops.

**Recommended approach when the time comes:**
1. Add `ResultType TypeKind` to `IRQuad` (set by irgen when it knows the result type)
2. In the code generators, after emitting arithmetic for a `TypeInt` result, emit
   `SXTW X0, W0` (ARM64) or `SBFM X0, X0, 0, 31` to truncate+sign-extend to 32 bits

This phase can be deferred until we're actually compiling a program that depends on
32-bit int overflow. bc, lua, and micropython don't heavily rely on overflow being
exactly 32-bit for correctness (they use it mostly for sizing).

---

## What NOT to change

- `long` stays 8 bytes — correct.
- Local variable *frame slots* stay 8-byte aligned — changing this would require
  sub-word stack slot tracking, not worth it.
- The gaston calling convention: `int` args are already passed in 64-bit registers
  per AAPCS64 (which zero/sign-extends narrow types). `va_arg` for `int` reads 8
  bytes and narrows — this is correct per ABI.
- `char` (1 byte), `short` (2 bytes), `float` (4 bytes), `double` (8 bytes) — all
  already correct, no change.

## Test progression

Run `go test ./cmd/gaston/ -run TestDockerRun -v` after each phase. The existing
tests should all continue to pass — this is a backwards-compatible change since
programs that only use `int` as a standalone local variable (no struct fields, no
`int*` derefs) are unaffected by Phases 1-3.

The programs that break without this fix are those with `struct { int ...; }` fields
at non-zero offsets, which currently get wrong offsets.
