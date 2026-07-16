# musl overlay (tracked)

Companion to the vendored, gitignored `musl/` source tree (musl 1.2.5).
Per design/musl-support-plan.md (plan v2, D5): everything gaston adds or
substitutes for the musl build lives here — never patch `musl/` in place.

- `generated/` — output of musl's `./configure --disable-shared` +
  `make obj/include/bits/{alltypes,syscall}.h`, run in an aarch64 Alpine
  container (2026-07-16, musl 1.2.5). Regenerate with the same command if
  the musl version changes.
- (M3 will add) `arch/` — substituted arch headers (syscall_arch.h,
  atomic_arch.h, pthread_arch.h) declaring extern primitives that the
  linker emits natively.
