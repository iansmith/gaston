#ifndef _SETJMP_H_
#define _SETJMP_H_

/* AArch64 jmp_buf: 22 × 8-byte slots.
 * Slots 0–9: X19–X28 (integer callee-saved)
 * Slot 10: X29 (FP)
 * Slot 11: X30 (LR / return address)
 * Slot 12: SP
 * Slots 13–21: reserved (d8–d15 not saved by gaston runtime)
 */
typedef long long jmp_buf[22];

int  setjmp(jmp_buf env);
void longjmp(jmp_buf env, int val);

#endif /* _SETJMP_H_ */
