# GCC Compatibility Gaps in Gaston

This document tracks the GCC extensions and standard C features that gaston needs
to support in order to compile codebases like MicroPython and picolibc that assume
a GCC-compatible compiler.

---

## GCC Extensions

### 1. `__attribute__((...))` — GCC Attribute Syntax

**What it is:**
A GCC extension that attaches metadata to declarations. Syntax is
`__attribute__((name))` or `__attribute__((name(args)))`, appearing after a
declarator or type. Multiple attributes can be stacked:
`__attribute__((packed, aligned(4)))`.

**Why it matters:**
Used pervasively in both picolibc and MicroPython. Without at minimum parsing
it, gaston will fail on nearly every picolibc header.

**Attributes gaston needs to handle:**

| Attribute | Action Required | Notes |
|---|---|---|
| `packed` | **Implement semantically** | Removes padding from struct fields; affects sizeof and field offsets. Wire-format structs in MicroPython depend on this being correct. |
| `aligned(N)` | Implement or no-op | Affects struct/variable alignment. Important for GC heap objects. |
| `noreturn` | No-op (parse only) | Marks functions that never return (e.g. `mp_raise_*`). No codegen effect needed. |
| `unused` | No-op | Suppresses unused-variable warnings. Gaston doesn't warn anyway. |
| `section("name")` | No-op initially | Places symbol in named ELF section. Needed for real bare-metal port placement; can ignore for now. |
| `weak` | No-op initially | Marks symbol as weak (overridable at link time). Without it, duplicate definitions cause link errors; with it as no-op, link errors become incorrect silences. Flag for later. |
| `constructor` | No-op initially | Registers function to run before `main`. Can replace with explicit init calls. |
| `format(printf, m, n)` | No-op | Enables compile-time format-string checking. Pure hint, no codegen. |
| `visibility("default"/"hidden")` | No-op | ELF symbol visibility. Not relevant for static linking. |
| `noinline` | No-op | Prevents inlining. Gaston doesn't inline anyway. |
| `pure` / `const` | No-op | Optimization hints (no side effects). |
| `malloc` | No-op | Hints that return value is a fresh non-aliased pointer. |
| `cold` / `hot` | No-op | Branch prediction hints. |
| `used` | No-op | Prevents dead-code elimination of a symbol. |
| `deprecated` | No-op | Emits warning on use. |
| `alias("sym")` | No-op initially | Makes symbol an alias for another. Used in C API wrappers. |

**Implementation approach:**
Parse `__attribute__((...))` wherever a type qualifier or declaration modifier
can appear (after declarator, after type keyword, after closing paren of a
function declaration). For most attributes, discard. For `packed`, record on
the struct/union type and apply zero-padding layout.

---

### 2. Computed Gotos — `&&label` and `goto *ptr`

**What it is:**
GCC extension that allows taking the address of a label as a `void*`, then
jumping to it indirectly:

```c
void *dispatch_table[] = { &&op_load, &&op_store, &&op_add };
goto *dispatch_table[opcode];

op_load:
    ...
```

**Why it matters:**
Used in MicroPython's bytecode interpreter (`vm.c`) as the dispatch mechanism.
Gives roughly 10–15% interpreter speedup over a switch statement by avoiding
the branch table indirection. Can be disabled with
`MICROPY_OPT_COMPUTED_GOTO=0`, which falls back to a `switch` statement.

**Implementation approach:**
Two new constructs in the parser:
1. `&&identifier` as a unary expression — evaluates to `void*` (the address of
   the label in the generated code).
2. `goto *expr` as a statement — indirect branch to the address in `expr`.

In codegen, `&&label` emits an `ADR` (or `ADRP+ADD`) instruction loading the
label's address. `goto *expr` emits a `BR Xn` (indirect branch register).

**Workaround if deferred:** Define `MICROPY_OPT_COMPUTED_GOTO=0` in the port
config. Correctness is unaffected; only performance suffers.

---

### 3. `##__VA_ARGS__` — Empty Variadic Argument Suppression

**What it is:**
Standard C99 allows variadic macros with `__VA_ARGS__`, but if the macro is
called with no variadic arguments, a trailing comma is left behind:

```c
#define LOG(fmt, ...) printf(fmt, __VA_ARGS__)
LOG("hello")   // expands to: printf("hello",)  ← syntax error
```

GCC extension: prefixing with `##` suppresses the comma when `__VA_ARGS__` is
empty:

```c
#define LOG(fmt, ...) printf(fmt, ##__VA_ARGS__)
LOG("hello")        // expands to: printf("hello")    ← correct
LOG("val=%d", x)    // expands to: printf("val=%d", x) ← correct
```

C23 standardizes this differently via `__VA_OPT__`, but picolibc and MicroPython
use the GCC `##` form.

**Why it matters:**
Used in picolibc's own internal headers for debug/assert macros. Gaston already
supports `__VA_ARGS__` in variadic macros; this is an additional special case in
the token-paste (`##`) logic.

**Implementation approach:**
In the preprocessor's token-paste handler: when the right-hand operand of `##`
is the `__VA_ARGS__` token and it expands to an empty token list, delete both
the `##` and the preceding comma token (if any). This is a targeted special
case in the existing paste logic.

---

### 4. `__extension__`

**What it is:**
A GCC prefix keyword that suppresses warnings about the following expression or
declaration using a non-standard extension:

```c
__extension__ typedef long long __int64_t;
__extension__ ({ int x = 1; x + 1; });  // statement expression
```

**Why it matters:**
Appears in picolibc headers to guard declarations that use GCC-specific types
or statement expressions without triggering `-pedantic` warnings. Without
parsing it, gaston sees an unexpected token.

**Implementation approach:**
Treat as a no-op keyword — parse and discard. No semantic effect.

---

### 5. `__typeof__` as Alias for `typeof`

**What it is:**
GCC spells the `typeof` extension with leading/trailing double-underscores:
`__typeof__(expr)`. Both spellings are equivalent.

**Why it matters:**
Picolibc and MicroPython use both spellings in headers and macro expansions.
Gaston implements `typeof(...)` already; `__typeof__(...)` is just an alias.

**Implementation approach:**
In the lexer or preprocessor, map `__typeof__` and `__typeof` to the same token
as `typeof`. One-line fix.

---

### 6. `__has_include(...)`

**What it is:**
A preprocessor operator (GCC/Clang) that tests whether a header file can be
found on the include path, for use in `#if` expressions:

```c
#if __has_include(<stdatomic.h>)
#  include <stdatomic.h>
#endif
```

**Why it matters:**
Used in MicroPython port headers to conditionally include platform-specific
headers. Without it, `#if __has_include(...)` is a preprocessor error.

**Implementation approach:**
Handle in the `#if` expression evaluator. `__has_include(<name>)` and
`__has_include("name")` should search the include path and return 1 or 0.
Similar to `defined(...)` — a special form in the expression parser.

---

### 7. `__builtin_unreachable()`

**What it is:**
Tells the compiler that a code path is never reached. The compiler can use this
to eliminate dead code and suppress "control reaches end of non-void function"
warnings. Calling it if execution actually reaches it is undefined behavior.

```c
switch (op) {
    case OP_ADD: ...; break;
    case OP_SUB: ...; break;
    default: __builtin_unreachable();
}
```

**Why it matters:**
Used in MicroPython on exhaustive switches and after `mp_raise_*` calls to
signal to the compiler that those paths terminate. Without it, the compiler may
emit spurious warnings or fail to recognize the unreachable path.

**Implementation approach:**
Implement as a built-in that is a no-op at the call site — generates no code.
Optionally emit an `BRK #0` (ARM64 breakpoint) in debug builds to catch
violations.

---

### 8. `__builtin_frame_address(N)`

**What it is:**
Returns the frame pointer (base pointer) of the Nth calling frame. `N=0` means
the current frame's frame pointer.

```c
void *fp = __builtin_frame_address(0);
```

**Why it matters:**
Used in MicroPython's GC to find the top of the C stack so it can scan for
heap pointers that live in local variables. If this returns the wrong value (or
zero), the GC will miss stack roots and prematurely collect live objects,
causing memory corruption.

**Implementation approach:**
For `N=0` on ARM64: emit `MOV X0, FP` (move the frame pointer register into
the return register). For `N > 0`, each level requires following the saved
frame pointer chain — can restrict to `N=0` only initially, as that is the only
use in MicroPython.

---

## Standard C Gaps

### 9. `restrict` Keyword (C99)

**What it is:**
A type qualifier for pointers asserting that the pointed-to object is not
aliased by any other pointer in the current scope. It is a programmer-supplied
optimization hint; the compiler may generate better code but is not required to.
Incorrect use (lying about aliasing) is undefined behavior.

```c
void *memcpy(void *restrict dst, const void *restrict src, size_t n);
```

**Why it matters:**
Picolibc's `string.h`, `stdlib.h`, and `stdio.h` use `restrict` in function
declarations throughout. Without recognizing it, gaston will fail to parse
these headers.

**Implementation approach:**
Add `restrict` (and `__restrict`, `__restrict__` — GCC spellings) as a
recognized type qualifier in the parser. No codegen effect — treat identically
to `volatile` in terms of being parsed and discarded.

---

### 10. `_Static_assert` / `static_assert` (C11)

**What it is:**
A compile-time assertion. If the constant expression is false, the compiler
emits an error with the provided message. Unlike `assert()`, it produces no
runtime code.

```c
_Static_assert(sizeof(int) == 4, "int must be 4 bytes");
static_assert(sizeof(void*) == 8, "expected 64-bit pointers");  // C11 macro alias
```

**Why it matters:**
Used in picolibc and MicroPython for compile-time layout and size checks.
Picolibc's `assert.h` maps `static_assert` → `_Static_assert`, so the compiler
keyword itself must be recognized.

**Implementation approach:**
Add `_Static_assert(expr, string)` as a statement-level construct in the
parser. Evaluate `expr` as a constant expression at compile time; if zero,
emit a compile error with the message string. No code emitted otherwise.

---

### 11. `_Thread_local` / `__thread` (C11 / GCC)

**What it is:**
A storage-class specifier that gives each thread its own instance of a variable.
`_Thread_local` is the C11 keyword; `__thread` is the older GCC spelling.

```c
_Thread_local int errno_val;
__thread struct _reent *_reent_ptr;
```

**Why it matters:**
Used in picolibc's re-entrant libc state (`struct _reent`) and in MicroPython's
threading support. On a bare-metal single-core target there is only one thread,
so thread-local storage is equivalent to a plain global variable.

**Implementation approach:**
Parse `_Thread_local` and `__thread` as storage-class specifiers. For bare-metal
single-core: treat as `static` (one instance, globally). No TLS segment
machinery needed.

---

### 12. `_Alignas` / `_Alignof` (C11)

**What it is:**
`_Alignof(type)` returns the alignment requirement of a type as a compile-time
constant (similar to `sizeof`). `_Alignas(N)` or `_Alignas(type)` specifies
the alignment of a variable or struct member.

```c
_Alignas(16) char buf[64];
size_t a = _Alignof(double);
```

C11 also defines `alignas` and `alignof` as macro aliases in `<stdalign.h>`.

**Why it matters:**
Used in picolibc for GC heap alignment and in some MicroPython object layouts.

**Implementation approach:**
`_Alignof(type)` — implement as a compile-time constant expression, like
`sizeof`. `_Alignas(N)` — record on the variable/field declaration and
enforce the alignment in the stack frame or data section layout.

---

## Summary Table

| Gap | Kind | Effort | Can Defer? |
|---|---|---|---|
| `__attribute__((...))` parse + no-op | GCC ext | Low | No — blocks header parsing |
| `__attribute__((packed))` semantics | GCC ext | Medium | No — affects struct layout |
| `restrict` / `__restrict` as no-op qualifier | C99 | Low | No — blocks header parsing |
| `__typeof__` alias | GCC ext | Trivial | No — in picolibc headers |
| `__extension__` no-op | GCC ext | Trivial | No — in picolibc headers |
| `_Static_assert` statement | C11 | Low | No — compile errors otherwise |
| `##__VA_ARGS__` empty suppression | GCC ext | Low | No — in picolibc headers |
| `__has_include(...)` | GCC/Clang | Low | No — in MicroPython port headers |
| `__builtin_unreachable()` no-op | GCC builtin | Trivial | Yes — warnings only |
| `__builtin_frame_address(0)` | GCC builtin | Low | No — GC correctness |
| `_Thread_local` / `__thread` as static | C11 / GCC ext | Low | Yes — single-core bare-metal |
| `_Alignas` / `_Alignof` | C11 | Medium | Yes — for initial port |
| Computed gotos (`&&label`, `goto *ptr`) | GCC ext | High | Yes — disable with config flag |
| `stdatomic.h` stub | C11 | Low | Yes — single-core bare-metal |
