/* sscanf.cm — gaston sscanf implementation.
 *
 * Algorithm derived from musl libc (MIT License)
 * https://musl.libc.org / https://git.musl-libc.org/cgit/musl
 * Relevant files: src/stdio/vsscanf.c, src/stdio/vfscanf.c,
 *                 src/internal/intscan.c
 *
 * Key adaptations for gaston C-minus:
 *   - No FILE* / shgetc infrastructure; work directly on char* indices
 *   - No wchar / %[ scanset support (not needed yet)
 *   - bool → int; static/inline removed; size_t → long
 *   - va_list is long* and va_arg is a gaston compiler built-in
 */

#include <stdarg.h>

/* ── character classification helpers ──────────────────────────────────── */

int __sc_isws(int c) {
    if (c == ' ')  { return 1; }
    if (c == '\t') { return 1; }
    if (c == '\n') { return 1; }
    if (c == '\r') { return 1; }
    return 0;
}

int __sc_isdigit(int c) {
    if ((c >= '0') & (c <= '9')) { return 1; }
    return 0;
}

int __sc_isxdigit(int c) {
    if ((c >= '0') & (c <= '9')) { return 1; }
    if ((c >= 'a') & (c <= 'f')) { return 1; }
    if ((c >= 'A') & (c <= 'F')) { return 1; }
    return 0;
}

/* xdigit value 0-15 */
int __sc_xval(int c) {
    if ((c >= '0') & (c <= '9')) { return c - '0'; }
    if ((c >= 'a') & (c <= 'f')) { return c - 'a' + 10; }
    return c - 'A' + 10;
}

/* ── skip whitespace in str starting at si, return new si ──────────────── */
int __sc_skipws(char* str, int si) {
    while (__sc_isws(str[si])) {
        si = si + 1;
    }
    return si;
}

/* ── scan integer in the given base (2/8/10/16).
   Handles optional leading sign for signed mode.
   P2-C: clamps signed decimal values to [LLONG_MIN, LLONG_MAX] on overflow.
   Adapted from musl __intscan() logic. ─────────────────────────────────── */
int __sc_scan_int(char* str, int si, int base, int is_signed,
                  long* out_val, int* matched) {
    long val;
    int neg;
    int digit;
    int any;
    int overflow;
    int c;

    neg = 0;
    any = 0;
    overflow = 0;
    val = 0;

    /* sign (for signed types) */
    if (is_signed) {
        if (str[si] == '-') { neg = 1; si = si + 1; }
        else if (str[si] == '+') { si = si + 1; }
    }

    /* auto-detect base from prefix */
    if (base == 0) {
        if (str[si] == '0') {
            si = si + 1;
            if ((str[si] == 'x') | (str[si] == 'X')) {
                base = 16;
                si = si + 1;
            } else {
                base = 8;
                /* The '0' we consumed counts as a digit */
                any = 1;
            }
        } else {
            base = 10;
        }
    } else if (base == 16) {
        /* skip optional 0x/0X prefix */
        if ((str[si] == '0') & ((str[si+1] == 'x') | (str[si+1] == 'X'))) {
            si = si + 2;
        }
    }

    while (str[si] != 0) {
        c = str[si];
        if ((base == 10) & __sc_isdigit(c)) {
            digit = c - '0';
        } else if ((base == 16) & __sc_isxdigit(c)) {
            digit = __sc_xval(c);
        } else if ((base == 8) & (c >= '0') & (c <= '7')) {
            digit = c - '0';
        } else if ((base == 2) & ((c == '0') | (c == '1'))) {
            digit = c - '0';
        } else {
            break;
        }
        /* P2-C: detect signed decimal overflow before accumulating */
        if (is_signed & (base == 10) & !overflow) {
            if (val > (9223372036854775807 - digit) / 10) {
                overflow = 1;
            }
        }
        if (!overflow) {
            val = val * base + digit;
        }
        si = si + 1;
        any = 1;
    }

    if (overflow) {
        if (neg) {
            val = 0 - 9223372036854775807 - 1;  /* LLONG_MIN */
        } else {
            val = 9223372036854775807;            /* LLONG_MAX */
        }
        neg = 0;  /* already applied sign */
    }
    if (neg) { val = 0 - val; }
    *out_val = val;
    *matched = any;
    return si;
}

/* ── check whether character c is in the scanset fmt[pat_start..pat_end).
   pat_start/pat_end are byte indices into fmt (pat_end = index of closing ]).
   Handles: literal chars, ranges (x-y), and ] as first char (literal).   ── */
int __sc_in_set(char* fmt, int pat_start, int pat_end, int c) {
    int pi;
    pi = pat_start;
    /* ] as the very first character of the set is treated as a literal ] */
    if ((pi < pat_end) & (fmt[pi] == ']')) {
        if (c == ']') { return 1; }
        pi = pi + 1;
    }
    while (pi < pat_end) {
        /* range x-y: need at least 3 chars left and middle char is '-' */
        if ((pi + 2 < pat_end) & (fmt[pi + 1] == '-')) {
            if ((c >= fmt[pi]) & (c <= fmt[pi + 2])) { return 1; }
            pi = pi + 3;
        } else {
            if (c == fmt[pi]) { return 1; }
            pi = pi + 1;
        }
    }
    return 0;
}

/* ── scan a floating-point number from str[si..].
   P1-A: accumulate integer and fractional parts as long integers, then
   combine with one division — avoids accumulated error from repeated *0.1.
   P1-B: recognise inf/infinity/nan (case-insensitive).
   Returns new si.  out_val receives the parsed double. ────────────────── */
int __sc_scan_float(char* str, int si, double* out_val, int* matched) {
    long int_part, frac_part, frac_scale;
    int frac_digits, in_frac;
    int neg, any;
    int exp_neg, exp_val;
    int c;
    double val, zero;

    neg = 0;
    any = 0;
    *matched = 0;

    if (str[si] == '-') { neg = 1; si = si + 1; }
    else if (str[si] == '+') { si = si + 1; }

    /* P1-B: inf / infinity (case-insensitive) */
    c = str[si];
    if ((c == 'i') | (c == 'I')) {
        if (((str[si+1] == 'n') | (str[si+1] == 'N')) &
            ((str[si+2] == 'f') | (str[si+2] == 'F'))) {
            zero = 0.0;
            val = 1.0 / zero;
            if (neg) { val = 0.0 - val; }
            *out_val = val;
            *matched = 1;
            si = si + 3;
            /* optionally consume "inity" to accept "infinity" */
            if (((str[si]   == 'i') | (str[si]   == 'I')) &
                ((str[si+1] == 'n') | (str[si+1] == 'N')) &
                ((str[si+2] == 'i') | (str[si+2] == 'I')) &
                ((str[si+3] == 't') | (str[si+3] == 'T')) &
                ((str[si+4] == 'y') | (str[si+4] == 'Y'))) {
                si = si + 5;
            }
            return si;
        }
    }

    /* P1-B: nan (case-insensitive; sign ignored per C standard) */
    if ((c == 'n') | (c == 'N')) {
        if (((str[si+1] == 'a') | (str[si+1] == 'A')) &
            ((str[si+2] == 'n') | (str[si+2] == 'N'))) {
            zero = 0.0;
            *out_val = zero / zero;
            *matched = 1;
            return si + 3;
        }
    }

    /* P1-A: accumulate as integers for precision */
    int_part = 0;
    frac_part = 0;
    frac_scale = 1;
    frac_digits = 0;
    in_frac = 0;

    while (str[si] != 0) {
        c = str[si];
        if (__sc_isdigit(c)) {
            if (in_frac) {
                if (frac_digits < 18) {
                    frac_part = frac_part * 10 + (c - '0');
                    frac_scale = frac_scale * 10;
                    frac_digits = frac_digits + 1;
                }
                /* else: ignore extra digits beyond double precision */
            } else {
                int_part = int_part * 10 + (c - '0');
            }
            any = 1;
            si = si + 1;
        } else if ((c == '.') & !in_frac) {
            in_frac = 1;
            si = si + 1;
        } else {
            break;
        }
    }

    if (!any) { *matched = 0; return si; }

    val = (double)int_part + (double)frac_part / (double)frac_scale;

    /* optional exponent: e+123 / e-456 / E+... */
    if ((str[si] == 'e') | (str[si] == 'E')) {
        si = si + 1;
        exp_neg = 0;
        exp_val = 0;
        if (str[si] == '-') { exp_neg = 1; si = si + 1; }
        else if (str[si] == '+') { si = si + 1; }
        while (__sc_isdigit(str[si])) {
            exp_val = exp_val * 10 + (str[si] - '0');
            si = si + 1;
        }
        if (exp_neg) {
            while (exp_val > 0) { val = val / 10.0; exp_val = exp_val - 1; }
        } else {
            while (exp_val > 0) { val = val * 10.0; exp_val = exp_val - 1; }
        }
    }

    if (neg) { val = 0.0 - val; }
    *out_val = val;
    *matched = any;
    return si;
}

/* ── sscanf: scan formatted input from str according to fmt.
   Adapted from musl vsscanf / vfscanf flow. ─────────────────────────────── */
int sscanf(char* str, char* fmt, ...) {
    va_list ap;
    int fi;
    int si;
    int count;
    int suppress;
    int base;
    int c;
    long ival;
    double dval;
    int matched;
    long* idest;
    double* ddest;
    char* sdest;
    int di;
    int width;

    ap    = __va_start();
    fi    = 0;
    si    = 0;
    count = 0;

    while (fmt[fi] != 0) {

        /* whitespace in format: skip any whitespace in input */
        if (__sc_isws(fmt[fi])) {
            si = __sc_skipws(str, si);
            fi = fi + 1;
            continue;
        }

        /* literal match (non-%) */
        if (fmt[fi] != '%') {
            if (str[si] == fmt[fi]) {
                si = si + 1;
                fi = fi + 1;
                continue;
            } else {
                break;  /* mismatch — stop scanning */
            }
        }

        /* '%' — format specifier */
        fi = fi + 1;
        if (fmt[fi] == 0) { break; }

        /* assignment suppression */
        suppress = 0;
        if (fmt[fi] == '*') {
            suppress = 1;
            fi = fi + 1;
            if (fmt[fi] == 0) { break; }
        }

        /* field width */
        width = 0;
        while (__sc_isdigit(fmt[fi])) {
            width = width * 10 + (fmt[fi] - '0');
            fi = fi + 1;
        }
        if (fmt[fi] == 0) { break; }

        /* length modifier (l, ll, h, hh — no-op in 64-bit gaston) */
        if ((fmt[fi] == 'l') | (fmt[fi] == 'h') | (fmt[fi] == 'z') | (fmt[fi] == 'j')) {
            fi = fi + 1;
            if ((fmt[fi] == 'l') | (fmt[fi] == 'h')) { fi = fi + 1; }
        }
        if (fmt[fi] == 0) { break; }

        c = fmt[fi];
        fi = fi + 1;

        /* %d — signed decimal */
        if (c == 'd') {
            si = __sc_skipws(str, si);
            si = __sc_scan_int(str, si, 10, 1, &ival, &matched);
            if (!matched) { break; }
            if (!suppress) {
                idest  = va_arg(ap, long*);
                *idest = ival;
                count  = count + 1;
            }

        /* %i — auto-detect base */
        } else if (c == 'i') {
            si = __sc_skipws(str, si);
            si = __sc_scan_int(str, si, 0, 1, &ival, &matched);
            if (!matched) { break; }
            if (!suppress) {
                idest  = va_arg(ap, long*);
                *idest = ival;
                count  = count + 1;
            }

        /* %u — unsigned decimal */
        } else if (c == 'u') {
            si = __sc_skipws(str, si);
            si = __sc_scan_int(str, si, 10, 0, &ival, &matched);
            if (!matched) { break; }
            if (!suppress) {
                idest  = va_arg(ap, long*);
                *idest = ival;
                count  = count + 1;
            }

        /* %x / %X — unsigned hex */
        } else if ((c == 'x') | (c == 'X')) {
            si = __sc_skipws(str, si);
            si = __sc_scan_int(str, si, 16, 0, &ival, &matched);
            if (!matched) { break; }
            if (!suppress) {
                idest  = va_arg(ap, long*);
                *idest = ival;
                count  = count + 1;
            }

        /* %o — unsigned octal */
        } else if (c == 'o') {
            si = __sc_skipws(str, si);
            si = __sc_scan_int(str, si, 8, 0, &ival, &matched);
            if (!matched) { break; }
            if (!suppress) {
                idest  = va_arg(ap, long*);
                *idest = ival;
                count  = count + 1;
            }

        /* %f / %g / %e / %F / %G / %E — floating point */
        } else if ((c == 'f') | (c == 'F') | (c == 'g') | (c == 'G') | (c == 'e') | (c == 'E')) {
            si = __sc_skipws(str, si);
            si = __sc_scan_float(str, si, &dval, &matched);
            if (!matched) { break; }
            if (!suppress) {
                ddest  = va_arg(ap, double*);
                *ddest = dval;
                count  = count + 1;
            }

        /* %s — non-whitespace word */
        } else if (c == 's') {
            si = __sc_skipws(str, si);
            if (!suppress) {
                sdest = va_arg(ap, char*);
                di = 0;
                while ((str[si] != 0) & (__sc_isws(str[si]) == 0)) {
                    if ((width == 0) | (di < width - 1)) {
                        sdest[di] = str[si];
                        di = di + 1;
                    }
                    si = si + 1;
                }
                sdest[di] = 0;
                if (di > 0) { count = count + 1; }
            } else {
                while ((str[si] != 0) & (__sc_isws(str[si]) == 0)) {
                    si = si + 1;
                }
            }

        /* %c — single character (does NOT skip whitespace) */
        } else if (c == 'c') {
            if (str[si] == 0) { break; }
            if (!suppress) {
                idest  = va_arg(ap, long*);
                *idest = str[si];
                count  = count + 1;
            }
            si = si + 1;

        /* %[...] — scanset: consume chars matching (or not matching) the set.
           Does NOT skip leading whitespace.
           [ may be followed by ^ for negation.
           ] as the first char (after optional ^) is literal.              */
        } else if (c == '[') {
            int negate, pat_start, pat_end, in_set;
            negate = 0;
            if (fmt[fi] == '^') { negate = 1; fi = fi + 1; }
            pat_start = fi;
            /* ] as first char is literal — consume it before the search loop */
            if (fmt[fi] == ']') { fi = fi + 1; }
            /* find the closing ] */
            while ((fmt[fi] != 0) & (fmt[fi] != ']')) { fi = fi + 1; }
            if (fmt[fi] == 0) { break; }   /* malformed format — stop */
            pat_end = fi;
            fi = fi + 1;                   /* skip closing ] */

            if (!suppress) {
                sdest = va_arg(ap, char*);
                di = 0;
                while (str[si] != 0) {
                    in_set = __sc_in_set(fmt, pat_start, pat_end, str[si]);
                    if (negate) { in_set = !in_set; }
                    if (!in_set) { break; }
                    if ((width == 0) | (di < width - 1)) {
                        sdest[di] = str[si];
                        di = di + 1;
                    }
                    si = si + 1;
                }
                sdest[di] = 0;
                if (di == 0) { break; }    /* no match — input failure */
                count = count + 1;
            } else {
                while (str[si] != 0) {
                    in_set = __sc_in_set(fmt, pat_start, pat_end, str[si]);
                    if (negate) { in_set = !in_set; }
                    if (!in_set) { break; }
                    si = si + 1;
                }
            }

        /* %% — literal percent */
        } else if (c == '%') {
            if (str[si] == '%') {
                si = si + 1;
            } else {
                break;
            }
        }
    }

    /* P2-D: return EOF (-1) if string was exhausted before any conversion */
    if ((count == 0) & (str[si] == 0)) { return 0 - 1; }
    return count;
}
