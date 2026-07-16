# Gaston Musl Libc Support — Plan v2

**Goal:** musl 1.2.5, compiled by gaston, **fully replaces picolibc** as the libc for
mazarin userland — motivated by CPython-on-mazarin, which needs musl-grade completeness.
v1 target: **static, AArch64-only**, threads deferred to a later milestone.

**Revision note:** v1 of this plan (see git history) proposed a general inline-asm
engine (old Phase 4/5) and treated TLS as out of scope. Both were overturned in the
2026-07-16 grill session. The decision log below is authoritative.

---

## Decision log (grill session, 2026-07-16)

| # | Decision | Rationale |
|---|----------|-----------|
| D1 | **musl fully replaces picolibc** (end state; picolibc keeps serving until musl is proven) | one libc to maintain; CPython needs musl completeness |
| D2 | **musl owns ALL TLS init** — `__init_tp` for the main thread, `pthread_create` for the rest | kernel never computes TLS layout; `TPIDR_EL0` is EL0-writable; kernel-managed TLS is structurally incompatible with musl's `pthread_create`, which allocates stack+TLS+TCB in userspace and passes TP to `clone(CLONE_SETTLS)` |
| D3 | **Startup = linker-emitted `_start` shim** (`mov x0, sp; bl _start_c`), then musl C owns everything (`__libc_start_main`) | reuses the proven hand-encoded `_start` mechanism (linker.go); deletes old Phase 3 (file-scope `__asm__`) entirely |
| D4 | **Arch primitives = extern functions with linker-emitted bodies** (the `emitPosixSyscalls`/`emitSetjmpFns` pattern) | zero compiler-frontend changes; ~10 new encoders written once in Go; out-of-line cost negligible; builtin-inlining upgrade path stays open |
| D5 | **Hybrid build**: configure output committed to tracked overlay; host Go-test error-bucket harness over vendored `musl/` for iteration; `third-party/musl/` container pipeline ships tracked `output/` artifacts | fast inner loop + reproducible artifacts, matching the picolibc pattern |
| D6 | **STV_HIDDEN deferred** to the `.so` milestone (backlog ticket; v1 plan §Phase 2 text preserved in git history) | zero effect on static-link correctness |
| D7 | **TLS exerciser** validates gaston `__thread` codegen + musl `__init_tp`; runs in the Docker/Alpine harness first, then the identical binary on mazarin | tests the path CPython will actually use; acceptance test for the TLS ticket chain |
| D8 | **Rebuild `libgastonc.a` now** (`task docker-picolibc`) and keep the picolibc feature-test net green throughout | the feature tests are the regression net under all upcoming compiler work |

### Codebase facts the plan relies on

- gaston does **not** assemble `.s` files — `libc/setjmp_arm64.s` is reference text; real
  code is hand-encoded words (`emitSetjmpFns`, elfgen.go). All "assembly" in this plan
  means encoded words emitted from Go.
- The linker already synthesizes `_start` (linker.go:356) and syscall stubs
  (`emitPosixSyscalls`, elfgen.go) using Linux syscall numbers via `SVC #0`.
- mazarin uses the **Linux syscall ABI** — "musl on Linux" and "musl on mazarin" do not
  diverge at the syscall layer.
- musl's `errno` is a `struct pthread` field reached via the thread pointer — **the
  TP-read primitive is on the libc critical path; full `__thread`/`.tdata` codegen is
  app-facing** (CPython, exerciser) and off the vertical-slice path.

---

## Mazarin kernel obligations

**Now (single-threaded milestones M0–M7):**
- Linux-style process entry stack: `argc`, `argv[]`, `envp[]`, auxv with at least
  `AT_PHDR`, `AT_PHENT`, `AT_PHNUM`, `AT_PAGESZ`, `AT_RANDOM`, `AT_ENTRY`, `AT_NULL`
  (musl's `__init_tp` locates `PT_TLS` via `AT_PHDR`).
- Do not trap EL0 writes to `TPIDR_EL0` (hardware default).
- The syscalls the slice exercises: `write`, `exit_group`, plus whatever stdio init
  touches (`ioctl`/`fcntl` tolerated as `-ENOSYS`).

**Threads milestone (M8):**
- `clone` with thread flags incl. `CLONE_SETTLS` (kernel copies musl's TP argument into
  the child's `TPIDR_EL0` — no layout logic), `CLONE_PARENT_SETTID`, `CLONE_CHILD_CLEARTID`.
- `futex` (WAIT/WAKE minimum), `mmap`/`munmap`, `set_tid_address`, thread-`exit`,
  `rt_sigprocmask`.

---

## Repository layout

- `musl/` — vendored musl 1.2.5 source, **gitignored** (re-downloadable; never edited).
- `third-party/musl/overlay/` — **tracked**: substituted arch headers, committed
  configure output (`bits/alltypes.h`, syscall numbers, `config.mak`), build scripts.
  Overlay include paths win over `musl/` by `-I` order.
- `third-party/musl/` — later, the container pipeline (`Dockerfile` + tracked
  `output/`: `libmusl.a`, headers), mirroring `third-party/picolibc/`.

---

## Milestones

### M0 — Test-net repair (GAST-3, in flight)
Harness path fix (link prebuilt archive; skip source-recompile tests on host) +
`task docker-picolibc` archive rebuild. Exit: `TestDockerRun` green incl.
`longdouble_size` (retroactive end-to-end check of GAST-1).

### M1 — Baseline
Run musl `./configure` once in a container (aarch64, static); commit generated headers
into the overlay. Build the host Go-test harness that compiles all ~1,530 `musl/src`
files in-process and buckets failures by error class. Exit: a ranked error-bucket
dashboard; the count is the project's progress metric.

### M2 — Compiler gaps (bucket-driven)
Fix largest buckets first, one GAST ticket per feature, each with an isolated
`testdata/` program. Expected from the v1 audit: `_Complex` no-panic qualifier,
`_Atomic` no-op qualifier, `__builtin_expect`, weak/alias dual-symbol emission
(parse exists; objgen side unverified), assorted attribute tolerance.

### M3 — Arch substitution
- Overlay `syscall_arch.h`: `__syscall0..6` as extern fns → linker-emitted
  `mov x8, #n; svc 0; ret` bodies (extend `emitPosixSyscalls`).
- Overlay `atomic_arch.h`: `a_cas`/`a_swap`/`a_fetch_add`/`a_barrier`… as extern fns →
  `ldaxr`/`stlxr` loops, `dmb ish`.
- Overlay `pthread_arch.h`: `__pthread_self` via extern TP-read (`mrs x0, tpidr_el0`).
- New encoders in `arm64enc.go`: `encLDAXR`/`encSTLXR` (32/64), `encDMBish`,
  `encMRS_tpidr_el0`, `encMSR_tpidr_el0`. (FP encoders from plan v1 dropped — exclude
  `musl/src/math/aarch64/*` and compile musl's generic C math.)
- Linker: musl-mode `_start` shim (`mov x0, sp; bl _start_c`); suppress the
  picolibc-flavored `_start`/syscall emission when targeting musl.

### M4 — Vertical slice
Static `hello` through real musl: `_start` shim → `__libc_start_main` (incl.
`__init_tp`) → `main` → `write(1, …)` → exit. Runs in the Alpine/arm64 Docker harness.
Exit: correct output, clean exit code. **This validates D2–D4 end to end.**

### M5 — TLS application support
- Parse `__thread`/`_Thread_local` as a real storage class (today: silently dropped —
  lexer `skipWords`).
- Emit `.tdata`/`.tbss` sections + `PT_TLS` program header; 16-byte-reserved TCB,
  Variant I layout; link-time tpoff resolution (static image — no TLS relocs needed).
- Local-Exec codegen: `mrs` + tpoff add for load/store/address-of.
- **TLS exerciser** in `testdata/` (D7): assorted sizes/alignments, layout asserts
  (address vs TP, tpoff arithmetic, alignment), persistence across calls, `&v` vs
  direct-access agreement. featureTests entry once musl links.

### M6 — Coverage
Drive the M1 bucket count to zero; archive `libmusl.a`; widen the test programs across
stdio/string/stdlib/time. Exit: full musl static archive builds; test suite green.

### M7 — Ship
`third-party/musl/` container pipeline producing tracked `output/` (libmusl.a, headers,
crt). Mazarin image consumes musl; kernel provides the M0-listed entry-stack/auxv
contract. TLS exerciser runs on mazarin unchanged.

### M8 — Threads (separate effort, scoped later)
Kernel: `clone`/`CLONE_SETTLS`/`futex` et al. musl: `src/thread/` compiles (needs a
`clone.s` equivalent — linker-emitted). Exerciser gains per-thread separation asserts.

---

## Deferred / backlog

- **STV_HIDDEN visibility** (D6) — backlog ticket at the `.so` milestone.
- **Dynamic linking / `ldso`** — out of scope for v1.
- **`_Complex` arithmetic** — `__STDC_NO_COMPLEX__` stays; keyword must not panic (M2).
- **Optimized aarch64 libm asm** — excluded; generic C math (revisit if perf demands).
- **x86-64** — separate Target-abstraction effort; deliberately not entangled here.

## Risk register (carried forward, updated)

| Risk | Mitigation |
|------|------------|
| weak/alias dual-symbol emission incomplete | surfaces in M1 buckets; fix in M2 with isolated test |
| musl internals need an asm form the substitution can't express | inventory during M3; the extern-fn pattern covers syscalls/atomics/TP; `clone.s` deferred to M8 |
| mazarin entry-stack/auxv contract drift | the M4 slice binary doubles as mazarin's conformance test in M7 |
| archive rot recurs (D8) | consider a Taskfile staleness check when it next bites |
