package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"
)

// ── data types ───────────────────────────────────────────────────────────────

// macroDef stores a preprocessor macro definition.
type macroDef struct {
	params   []string // nil = object-like; non-nil (possibly empty) = function-like
	variadic bool     // true if last formal is "..."
	body     string
}

// condFrame tracks one level of #ifdef / #ifndef nesting.
type condFrame struct {
	active bool // current branch is being compiled
	done   bool // a true branch has been seen (so #else becomes inactive)
}

// logLine is one logical source line after \ continuation joining.
type logLine struct {
	text  string
	count int // number of raw lines consumed (used to emit the right newlines)
}

// includeFlags is a flag.Value that accumulates -I paths.
type includeFlags []string

func (f *includeFlags) String() string        { return strings.Join(*f, ":") }
func (f *includeFlags) Set(v string) error    { *f = append(*f, v); return nil }

// defineFlags is a flag.Value that accumulates -D NAME[=value] defines.
type defineFlags []string

func (f *defineFlags) String() string        { return strings.Join(*f, " ") }
func (f *defineFlags) Set(v string) error    { *f = append(*f, v); return nil }

// builtinHeaders provides virtual content for standard headers that gaston
// implements internally rather than relying on a host system libc.
var builtinHeaders = map[string]string{
	"stdarg.h": `
/* gaston built-in <stdarg.h> */
typedef long* va_list;
#define va_start(ap, last)  ap = __va_start()
#define va_end(ap)
#define va_copy(dst, src)   ((dst) = (src))
`,
	"stddef.h": `
/* gaston built-in <stddef.h> */
typedef long size_t;
typedef long ptrdiff_t;
typedef long intptr_t;
#define NULL 0
#define offsetof(type, member) __builtin_offsetof(type, member)
`,
	"stdint.h": `
/* gaston built-in <stdint.h> */
typedef long          int64_t;
typedef unsigned long uint64_t;
typedef int           int32_t;
typedef unsigned int  uint32_t;
typedef long          intmax_t;
typedef unsigned long uintmax_t;
typedef long          ssize_t;
typedef long          size_t;
#define INT64_MAX  9223372036854775807
#define UINT64_MAX 18446744073709551615
#define INT32_MAX  2147483647
#define SIZE_MAX   18446744073709551615
`,
	"limits.h": `
/* gaston built-in <limits.h> */
#define CHAR_BIT   8
#define CHAR_MAX   127
#define CHAR_MIN   (-128)
#define UCHAR_MAX  255
#define SHRT_MAX   32767
#define SHRT_MIN   (-32768)
#define USHRT_MAX  65535
#define INT_MAX    2147483647
#define INT_MIN    (-2147483648)
#define UINT_MAX   4294967295
#define LONG_MAX   9223372036854775807
#define LONG_MIN   (-9223372036854775808)
#define ULONG_MAX  18446744073709551615
#define LLONG_MAX  9223372036854775807
#define LLONG_MIN  (-9223372036854775808)
#define ULLONG_MAX 18446744073709551615
`,
	"float.h": `
/* gaston built-in <float.h> */
#define DBL_MAX      1.7976931348623157e+308
#define DBL_MIN      2.2250738585072014e-308
#define DBL_EPSILON  2.2204460492503131e-16
#define FLT_MAX      3.4028235e+38
#define FLT_MIN      1.1754944e-38
#define FLT_EPSILON  1.1920929e-07
#define DBL_MANT_DIG 53
#define DBL_MAX_EXP  1024
#define DBL_MIN_EXP  (-1021)
#define FLT_MANT_DIG 24
#define FLT_MAX_EXP  128
#define FLT_MIN_EXP  (-125)
#define DECIMAL_DIG  17
`,
	"complex.h": `
/* gaston stub <complex.h> — _Complex is not supported.
   Defining __STDC_NO_COMPLEX__ causes conforming code to skip complex
   declarations; the empty guard here prevents parse errors if this header
   is included anyway. */
#ifndef _COMPLEX_H
#define _COMPLEX_H
#endif /* _COMPLEX_H */
`,
}

// ── preprocessor ─────────────────────────────────────────────────────────────

// preprocessor is a single-pass, line-oriented C preprocessor.
type preprocessor struct {
	defines      map[string]*macroDef
	includePaths []string
	inInclude    map[string]bool // files currently being processed (cycle detection)
	errors       int
}

// defaultLibcDir is the gaston standard library header directory, always
// searched last (after any caller-supplied paths) before the virtual fallback.
const defaultLibcDir = "libc"

// newPreprocessor creates a preprocessor with the given include search paths
// and extra command-line -D defines (each element is "NAME" or "NAME=value").
// The gaston libc directory ("libc") is always appended as the final search
// directory so that #include <stdarg.h>, <stddef.h>, etc. resolve to the
// real header files when running from the cmd/gaston working directory.
func newPreprocessor(includePaths []string, extraDefines []string) *preprocessor {
	paths := make([]string, len(includePaths), len(includePaths)+1)
	copy(paths, includePaths)
	// Append libc/ only if not already present.
	hasLibc := false
	for _, p := range paths {
		if p == defaultLibcDir {
			hasLibc = true
			break
		}
	}
	if !hasLibc {
		paths = append(paths, defaultLibcDir)
	}
	pp := &preprocessor{
		defines:      make(map[string]*macroDef),
		includePaths: paths,
		inInclude:    make(map[string]bool),
	}

	// Install predefined macros matching clang -target aarch64-linux-gnu built-ins.
	// Sections marked [gaston] differ intentionally from clang.
	builtinSrc := `
/* ── compiler identity ──────────────────────────────────────────────── */
#define __GASTON__              1
#define __GNUC__                4
#define __GNUC_MINOR__          2
#define __GNUC_PATCHLEVEL__     1
#define __GNUC_STDC_INLINE__    1
#define __GXX_ABI_VERSION       1002

/* ── standard C ─────────────────────────────────────────────────────── */
#define __STDC__                1
#define __STDC_VERSION__        201710L
#define __STDC_HOSTED__         1
#define __STDC_UTF_16__         1
#define __STDC_UTF_32__         1
#define __NO_INLINE__           1

/* ── target: AArch64 / Linux ─────────────────────────────────────────── */
#define __aarch64__             1
#define __AARCH64EL__           1
#define __ARM_64BIT_STATE       1
#define __ARM_ARCH              8
#define __ARM_ARCH_ISA_A64      1
#define __ARM_ARCH_PROFILE      'A'
#define __ARM_PCS_AAPCS64       1
#define __ARM_SIZEOF_WCHAR_T    4
#define __linux__               1
#define __linux                 1
#define linux                   1
#define __unix__                1
#define __unix                  1
#define unix                    1
#define __gnu_linux__           1
#define __ELF__                 1

/* ── ABI / data model (LP64) ─────────────────────────────────────────── */
#define __LP64__                1
#define _LP64                   1
#define __POINTER_WIDTH__       64
#define __BIGGEST_ALIGNMENT__   16
#define __CHAR_BIT__            8
#define __CHAR_UNSIGNED__       1

/* ── sizeof constants ────────────────────────────────────────────────── */
#define __SIZEOF_POINTER__      8
#define __SIZEOF_PTRDIFF_T__    8
#define __SIZEOF_SIZE_T__       8
#define __SIZEOF_LONG__         8
#define __SIZEOF_INT__          4
#define __SIZEOF_SHORT__        2
#define __SIZEOF_LONG_LONG__    8
#define __SIZEOF_FLOAT__        4
#define __SIZEOF_DOUBLE__       8
#define __SIZEOF_LONG_DOUBLE__  16
#define __SIZEOF_WCHAR_T__      4
#define __SIZEOF_WINT_T__       4
#define __SIZEOF_INT128__       16

/* ── integer type names ──────────────────────────────────────────────── */
#define __INT8_TYPE__           signed char
#define __INT16_TYPE__          short
#define __INT32_TYPE__          int
#define __INT64_TYPE__          long int
#define __UINT8_TYPE__          unsigned char
#define __UINT16_TYPE__         unsigned short
#define __UINT32_TYPE__         unsigned int
#define __UINT64_TYPE__         long unsigned int
#define __INT_FAST8_TYPE__      signed char
#define __INT_FAST16_TYPE__     short
#define __INT_FAST32_TYPE__     int
#define __INT_FAST64_TYPE__     long int
#define __INT_LEAST8_TYPE__     signed char
#define __INT_LEAST16_TYPE__    short
#define __INT_LEAST32_TYPE__    int
#define __INT_LEAST64_TYPE__    long int
#define __UINT_FAST8_TYPE__     unsigned char
#define __UINT_FAST16_TYPE__    unsigned short
#define __UINT_FAST32_TYPE__    unsigned int
#define __UINT_FAST64_TYPE__    long unsigned int
#define __UINT_LEAST8_TYPE__    unsigned char
#define __UINT_LEAST16_TYPE__   unsigned short
#define __UINT_LEAST32_TYPE__   unsigned int
#define __UINT_LEAST64_TYPE__   long unsigned int
#define __INTMAX_TYPE__         long int
#define __UINTMAX_TYPE__        long unsigned int
#define __INTPTR_TYPE__         long int
#define __UINTPTR_TYPE__        long unsigned int
#define __PTRDIFF_TYPE__        long int
#define __SIZE_TYPE__           long unsigned int
#define __WCHAR_TYPE__          unsigned int
#define __WINT_TYPE__           unsigned int
#define __SIG_ATOMIC_TYPE__     int
#define __CHAR16_TYPE__         unsigned short
#define __CHAR32_TYPE__         unsigned int

/* ── integer limits ──────────────────────────────────────────────────── */
#define __SCHAR_MAX__           127
#define __SHRT_MAX__            32767
#define __INT_MAX__             2147483647
#define __LONG_MAX__            9223372036854775807L
#define __LONG_LONG_MAX__       9223372036854775807LL
#define __WCHAR_MAX__           4294967295U
#define __WINT_MAX__            4294967295U
#define __SIZE_MAX__            18446744073709551615UL
#define __PTRDIFF_MAX__         9223372036854775807L
#define __INTMAX_MAX__          9223372036854775807L
#define __UINTMAX_MAX__         18446744073709551615UL
#define __INTPTR_MAX__          9223372036854775807L
#define __UINTPTR_MAX__         18446744073709551615UL
#define __INT8_MAX__            127
#define __INT16_MAX__           32767
#define __INT32_MAX__           2147483647
#define __INT64_MAX__           9223372036854775807L
#define __UINT8_MAX__           255
#define __UINT16_MAX__          65535
#define __UINT32_MAX__          4294967295U
#define __UINT64_MAX__          18446744073709551615UL
#define __INT_FAST8_MAX__       127
#define __INT_FAST16_MAX__      32767
#define __INT_FAST32_MAX__      2147483647
#define __INT_FAST64_MAX__      9223372036854775807L
#define __INT_LEAST8_MAX__      127
#define __INT_LEAST16_MAX__     32767
#define __INT_LEAST32_MAX__     2147483647
#define __INT_LEAST64_MAX__     9223372036854775807L
#define __UINT_FAST8_MAX__      255
#define __UINT_FAST16_MAX__     65535
#define __UINT_FAST32_MAX__     4294967295U
#define __UINT_FAST64_MAX__     18446744073709551615UL
#define __UINT_LEAST8_MAX__     255
#define __UINT_LEAST16_MAX__    65535
#define __UINT_LEAST32_MAX__    4294967295U
#define __UINT_LEAST64_MAX__    18446744073709551615UL
#define __SIG_ATOMIC_MAX__      2147483647
#define __INT_WIDTH__           32
#define __LONG_WIDTH__          64
#define __LLONG_WIDTH__         64
#define __SHRT_WIDTH__          16
#define __BOOL_WIDTH__          1
#define __WCHAR_WIDTH__         32
#define __WINT_WIDTH__          32
#define __SIZE_WIDTH__          64
#define __PTRDIFF_WIDTH__       64
#define __INTMAX_WIDTH__        64
#define __UINTMAX_WIDTH__       64
#define __INTPTR_WIDTH__        64
#define __UINTPTR_WIDTH__       64
#define __INT_FAST8_WIDTH__     8
#define __INT_FAST16_WIDTH__    16
#define __INT_FAST32_WIDTH__    32
#define __INT_FAST64_WIDTH__    64
#define __INT_LEAST8_WIDTH__    8
#define __INT_LEAST16_WIDTH__   16
#define __INT_LEAST32_WIDTH__   32
#define __INT_LEAST64_WIDTH__   64
#define __SIG_ATOMIC_WIDTH__    32

/* ── integer format strings (inttypes.h) ─────────────────────────────── */
#define __INT8_FMTd__           "hhd"
#define __INT8_FMTi__           "hhi"
#define __INT16_FMTd__          "hd"
#define __INT16_FMTi__          "hi"
#define __INT32_FMTd__          "d"
#define __INT32_FMTi__          "i"
#define __INT64_FMTd__          "ld"
#define __INT64_FMTi__          "li"
#define __UINT8_FMTo__          "hho"
#define __UINT8_FMTu__          "hhu"
#define __UINT8_FMTx__          "hhx"
#define __UINT8_FMTX__          "hhX"
#define __UINT16_FMTo__         "ho"
#define __UINT16_FMTu__         "hu"
#define __UINT16_FMTx__         "hx"
#define __UINT16_FMTX__         "hX"
#define __UINT32_FMTo__         "o"
#define __UINT32_FMTu__         "u"
#define __UINT32_FMTx__         "x"
#define __UINT32_FMTX__         "X"
#define __UINT64_FMTo__         "lo"
#define __UINT64_FMTu__         "lu"
#define __UINT64_FMTx__         "lx"
#define __UINT64_FMTX__         "lX"
#define __INT_FAST8_FMTd__      "hhd"
#define __INT_FAST8_FMTi__      "hhi"
#define __INT_FAST16_FMTd__     "hd"
#define __INT_FAST16_FMTi__     "hi"
#define __INT_FAST32_FMTd__     "d"
#define __INT_FAST32_FMTi__     "i"
#define __INT_FAST64_FMTd__     "ld"
#define __INT_FAST64_FMTi__     "li"
#define __INT_LEAST8_FMTd__     "hhd"
#define __INT_LEAST8_FMTi__     "hhi"
#define __INT_LEAST16_FMTd__    "hd"
#define __INT_LEAST16_FMTi__    "hi"
#define __INT_LEAST32_FMTd__    "d"
#define __INT_LEAST32_FMTi__    "i"
#define __INT_LEAST64_FMTd__    "ld"
#define __INT_LEAST64_FMTi__    "li"
#define __UINT_FAST8_FMTo__     "hho"
#define __UINT_FAST8_FMTu__     "hhu"
#define __UINT_FAST8_FMTx__     "hhx"
#define __UINT_FAST8_FMTX__     "hhX"
#define __UINT_FAST16_FMTo__    "ho"
#define __UINT_FAST16_FMTu__    "hu"
#define __UINT_FAST16_FMTx__    "hx"
#define __UINT_FAST16_FMTX__    "hX"
#define __UINT_FAST32_FMTo__    "o"
#define __UINT_FAST32_FMTu__    "u"
#define __UINT_FAST32_FMTx__    "x"
#define __UINT_FAST32_FMTX__    "X"
#define __UINT_FAST64_FMTo__    "lo"
#define __UINT_FAST64_FMTu__    "lu"
#define __UINT_FAST64_FMTx__    "lx"
#define __UINT_FAST64_FMTX__    "lX"
#define __UINT_LEAST8_FMTo__    "hho"
#define __UINT_LEAST8_FMTu__    "hhu"
#define __UINT_LEAST8_FMTx__    "hhx"
#define __UINT_LEAST8_FMTX__    "hhX"
#define __UINT_LEAST16_FMTo__   "ho"
#define __UINT_LEAST16_FMTu__   "hu"
#define __UINT_LEAST16_FMTx__   "hx"
#define __UINT_LEAST16_FMTX__   "hX"
#define __UINT_LEAST32_FMTo__   "o"
#define __UINT_LEAST32_FMTu__   "u"
#define __UINT_LEAST32_FMTx__   "x"
#define __UINT_LEAST32_FMTX__   "X"
#define __UINT_LEAST64_FMTo__   "lo"
#define __UINT_LEAST64_FMTu__   "lu"
#define __UINT_LEAST64_FMTx__   "lx"
#define __UINT_LEAST64_FMTX__   "lX"
#define __INTMAX_FMTd__         "ld"
#define __INTMAX_FMTi__         "li"
#define __UINTMAX_FMTo__        "lo"
#define __UINTMAX_FMTu__        "lu"
#define __UINTMAX_FMTx__        "lx"
#define __UINTMAX_FMTX__        "lX"
#define __INTPTR_FMTd__         "ld"
#define __INTPTR_FMTi__         "li"
#define __UINTPTR_FMTo__        "lo"
#define __UINTPTR_FMTu__        "lu"
#define __UINTPTR_FMTx__        "lx"
#define __UINTPTR_FMTX__        "lX"
#define __PTRDIFF_FMTd__        "ld"
#define __PTRDIFF_FMTi__        "li"
#define __SIZE_FMTo__           "lo"
#define __SIZE_FMTu__           "lu"
#define __SIZE_FMTx__           "lx"
#define __SIZE_FMTX__           "lX"
#define __INTMAX_C_SUFFIX__     L
#define __UINTMAX_C_SUFFIX__    UL
#define __INT64_C_SUFFIX__      L
#define __UINT32_C_SUFFIX__     U
#define __UINT64_C_SUFFIX__     UL

/* ── integer literal constructor macros ──────────────────────────────── */
#define __INT8_C(c)     c
#define __INT16_C(c)    c
#define __INT32_C(c)    c
#define __INT64_C(c)    c##L
#define __UINT8_C(c)    c
#define __UINT16_C(c)   c
#define __UINT32_C(c)   c##U
#define __UINT64_C(c)   c##UL
#define __INTMAX_C(c)   c##L
#define __UINTMAX_C(c)  c##UL

/* ── float constants ─────────────────────────────────────────────────── */
#define __FLT_RADIX__           2
#define __FLT_MANT_DIG__        24
#define __FLT_DIG__             6
#define __FLT_MIN_EXP__         (-125)
#define __FLT_MAX_EXP__         128
#define __FLT_MIN_10_EXP__      (-37)
#define __FLT_MAX_10_EXP__      38
#define __FLT_MAX__             3.40282347e+38F
#define __FLT_MIN__             1.17549435e-38F
#define __FLT_EPSILON__         1.19209290e-7F
#define __FLT_DENORM_MIN__      1.40129846e-45F
#define __FLT_HAS_DENORM__      1
#define __FLT_HAS_INFINITY__    1
#define __FLT_HAS_QUIET_NAN__   1
#define __FLT_DECIMAL_DIG__     9
#define __DBL_MANT_DIG__        53
#define __DBL_DIG__             15
#define __DBL_MIN_EXP__         (-1021)
#define __DBL_MAX_EXP__         1024
#define __DBL_MIN_10_EXP__      (-307)
#define __DBL_MAX_10_EXP__      308
#define __DBL_MAX__             1.7976931348623157e+308
#define __DBL_MIN__             2.2250738585072014e-308
#define __DBL_EPSILON__         2.2204460492503131e-16
#define __DBL_DENORM_MIN__      4.9406564584124654e-324
#define __DBL_HAS_DENORM__      1
#define __DBL_HAS_INFINITY__    1
#define __DBL_HAS_QUIET_NAN__   1
#define __DBL_DECIMAL_DIG__     17
#define __LDBL_MANT_DIG__       113
#define __LDBL_DIG__            33
#define __LDBL_MIN_EXP__        (-16381)
#define __LDBL_MAX_EXP__        16384
#define __LDBL_MIN_10_EXP__     (-4931)
#define __LDBL_MAX_10_EXP__     4932
#define __LDBL_MAX__            1.7976931348623157e+308
#define __LDBL_MIN__            2.2250738585072014e-308
#define __LDBL_EPSILON__        2.2204460492503131e-16
#define __LDBL_DENORM_MIN__     4.9406564584124654e-324
#define __LDBL_HAS_DENORM__     1
#define __LDBL_HAS_INFINITY__   1
#define __LDBL_HAS_QUIET_NAN__  1
#define __LDBL_DECIMAL_DIG__    36
#define __DECIMAL_DIG__         __LDBL_DECIMAL_DIG__
#define __FP_FAST_FMA           1
#define __FP_FAST_FMAF          1
#define __FINITE_MATH_ONLY__    0

/* ── endianness ──────────────────────────────────────────────────────── */
#define __ORDER_LITTLE_ENDIAN__ 1234
#define __ORDER_BIG_ENDIAN__    4321
#define __ORDER_PDP_ENDIAN__    3412
#define __BYTE_ORDER__          __ORDER_LITTLE_ENDIAN__
#define __LITTLE_ENDIAN__       1

/* ── GCC atomic lock-free guarantees ────────────────────────────────── */
#define __GCC_ATOMIC_BOOL_LOCK_FREE      2
#define __GCC_ATOMIC_CHAR_LOCK_FREE      2
#define __GCC_ATOMIC_CHAR16_T_LOCK_FREE  2
#define __GCC_ATOMIC_CHAR32_T_LOCK_FREE  2
#define __GCC_ATOMIC_SHORT_LOCK_FREE     2
#define __GCC_ATOMIC_INT_LOCK_FREE       2
#define __GCC_ATOMIC_LONG_LOCK_FREE      2
#define __GCC_ATOMIC_LLONG_LOCK_FREE     2
#define __GCC_ATOMIC_POINTER_LOCK_FREE   2
#define __GCC_ATOMIC_WCHAR_T_LOCK_FREE   2
#define __GCC_ATOMIC_TEST_AND_SET_TRUEVAL 1
#define __GCC_HAVE_SYNC_COMPARE_AND_SWAP_1  1
#define __GCC_HAVE_SYNC_COMPARE_AND_SWAP_2  1
#define __GCC_HAVE_SYNC_COMPARE_AND_SWAP_4  1
#define __GCC_HAVE_SYNC_COMPARE_AND_SWAP_8  1
#define __GCC_HAVE_SYNC_COMPARE_AND_SWAP_16 1
#define __ATOMIC_RELAXED        0
#define __ATOMIC_CONSUME        1
#define __ATOMIC_ACQUIRE        2
#define __ATOMIC_RELEASE        3
#define __ATOMIC_ACQ_REL        4
#define __ATOMIC_SEQ_CST        5

/* ── NULL / misc ─────────────────────────────────────────────────────── */
#define NULL                    0
#define __USER_LABEL_PREFIX__

/* ── Neutralise GCC-specific declaration attributes / extensions ─────── */
/* These would otherwise reach the parser and cause syntax errors.        */
#define __attribute__(x)
#define __attribute(x)
#define __asm__(x)
#define __asm(x)
#define __volatile__(x)
#define __noinline

/* ── Complex / imaginary types not supported ─────────────────────────── */
/* C11 §6.10.8.3: define these to signal no complex/imaginary support.   */
/* picolibc's build system probes for _Complex; a missing probe causes it */
/* to skip libm/complex/ entirely. We also provide a stub complex.h.     */
#define __STDC_NO_COMPLEX__   1
#define __STDC_NO_IMAGINARY__ 1

/* ── GCC branch-prediction hint ─────────────────────────────────────── */
/* Expand to the condition itself; the hint is discarded.                 */
#define __builtin_expect(x, hint) (x)

/* ── GCC NaN / Inf constructors ─────────────────────────────────────── */
/* 0.0/0.0 produces a quiet NaN at runtime on IEEE 754 hardware.         */
/* 1.0/0.0 produces +Inf.                                                */
#define __builtin_nanf(s)   (0.0/0.0)
#define __builtin_nan(s)    (0.0/0.0)
#define __builtin_nanl(s)   (0.0/0.0)
#define __builtin_nansf(s)  (0.0/0.0)
#define __builtin_inff()    (1.0/0.0)
#define __builtin_inf()     (1.0/0.0)
#define __builtin_huge_valf() (1.0/0.0)
#define __builtin_huge_val()  (1.0/0.0)

/* ── GCC floating-point comparison predicates ────────────────────────── */
#define __builtin_isless(x,y)        ((x) < (y))
#define __builtin_isgreater(x,y)     ((x) > (y))
#define __builtin_islessequal(x,y)   ((x) <= (y))
#define __builtin_isgreaterequal(x,y) ((x) >= (y))
#define __builtin_islessgreater(x,y) ((x) < (y) || (x) > (y))
#define __builtin_isunordered(x,y)   ((x) != (x) || (y) != (y))
`
	var dummy strings.Builder
	pp.processFile(builtinSrc, "<builtin>", &dummy)

	// Apply -D NAME or -D NAME=value defines from the command line.
	for _, d := range extraDefines {
		var defSrc string
		if idx := strings.IndexByte(d, '='); idx >= 0 {
			defSrc = "#define " + d[:idx] + " " + d[idx+1:] + "\n"
		} else {
			defSrc = "#define " + d + " 1\n"
		}
		pp.processFile(defSrc, "<cmdline>", &dummy)
	}

	return pp
}

// Preprocess runs the preprocessor on src (source file name file) and returns
// the expanded text, ready for lexing.
func (p *preprocessor) Preprocess(src, file string) (string, error) {
	src = stripBlockComments(src)
	var out strings.Builder
	p.processFile(src, file, &out)
	if p.errors > 0 {
		return "", fmt.Errorf("preprocessor: %d error(s)", p.errors)
	}
	return out.String(), nil
}

// stripBlockComments removes C block comments (/* ... */) from src, replacing
// each comment with a single space on the starting line and preserving any
// embedded newlines so that line numbers are not disturbed.
// String and character literals are not scanned for comment markers.
func stripBlockComments(src string) string {
	var out strings.Builder
	out.Grow(len(src))
	i := 0
	for i < len(src) {
		c := src[i]
		switch {
		case c == '"': // string literal — copy verbatim
			out.WriteByte(c)
			i++
			for i < len(src) {
				c = src[i]
				out.WriteByte(c)
				i++
				if c == '\\' && i < len(src) {
					out.WriteByte(src[i])
					i++
				} else if c == '"' {
					break
				}
			}
		case c == '\'': // char literal — copy verbatim
			out.WriteByte(c)
			i++
			for i < len(src) {
				c = src[i]
				out.WriteByte(c)
				i++
				if c == '\\' && i < len(src) {
					out.WriteByte(src[i])
					i++
				} else if c == '\'' {
					break
				}
			}
		case c == '/' && i+1 < len(src) && src[i+1] == '*': // block comment
			i += 2 // skip /*
			out.WriteByte(' ')
			for i < len(src) {
				if src[i] == '\n' {
					out.WriteByte('\n')
					i++
				} else if src[i] == '*' && i+1 < len(src) && src[i+1] == '/' {
					i += 2 // skip */
					break
				} else {
					i++
				}
			}
		default:
			out.WriteByte(c)
			i++
		}
	}
	return out.String()
}

func (p *preprocessor) errorf(file string, line int, format string, args ...any) {
	if line > 0 {
		fmt.Fprintf(os.Stderr, "%s:%d: %s\n", file, line, fmt.Sprintf(format, args...))
	} else {
		fmt.Fprintf(os.Stderr, "%s: %s\n", file, fmt.Sprintf(format, args...))
	}
	p.errors++
}

// processFile processes one source file, appending expanded text to out.
// It always starts with a fresh condition stack (outermost level active=true).
func (p *preprocessor) processFile(src, file string, out *strings.Builder) {
	if p.inInclude[file] {
		p.errorf(file, 0, "include cycle detected")
		return
	}
	p.inInclude[file] = true
	defer func() { delete(p.inInclude, file) }()

	conds := []condFrame{{active: true}}
	lineNum := 1

	for _, ll := range joinOpenLines(splitLogical(src)) {
		active := conds[len(conds)-1].active
		trimmed := strings.TrimSpace(ll.text)

		if strings.HasPrefix(trimmed, "#") {
			dir, rest := splitDirective(trimmed[1:])
			switch dir {
			case "ifdef", "ifndef":
				name := firstWord(rest)
				if name == "" {
					p.errorf(file, lineNum, "#%s: missing identifier", dir)
				} else {
					defined := p.defines[name] != nil
					entering := active && ((dir == "ifdef") == defined)
					conds = append(conds, condFrame{active: entering, done: entering})
				}

			case "else":
				if len(conds) <= 1 {
					p.errorf(file, lineNum, "#else without #ifdef/#ifndef")
				} else {
					top := &conds[len(conds)-1]
					parentActive := conds[len(conds)-2].active
					top.active = parentActive && !top.done
				}

			case "endif":
				if len(conds) <= 1 {
					p.errorf(file, lineNum, "#endif without #ifdef/#ifndef")
				} else {
					conds = conds[:len(conds)-1]
				}

			case "define":
				if active {
					p.parseDefine(rest, file, lineNum)
				}

			case "undef":
				if active {
					if name := firstWord(rest); name != "" {
						delete(p.defines, name)
					}
				}

			case "if":
				var entering bool
				if active {
					val := p.evalIfExpr(rest, file, lineNum)
					entering = val != 0
				}
				conds = append(conds, condFrame{active: active && entering, done: active && entering})

			case "elif":
				if len(conds) <= 1 {
					p.errorf(file, lineNum, "#elif without #if")
				} else {
					top := &conds[len(conds)-1]
					parentActive := conds[len(conds)-2].active
					if parentActive && !top.done {
						val := p.evalIfExpr(rest, file, lineNum)
						top.active = val != 0
						top.done = top.active
					} else {
						top.active = false
					}
				}

			case "include":
				if active {
					p.processInclude(rest, file, lineNum, out)
				}

			case "error":
				if active {
					p.errorf(file, lineNum, "#error %s", strings.TrimSpace(rest))
				}

			case "pragma", "warning":
				// silently ignore

			default:
				// Unknown directives are silently ignored (picolibc compatibility).
			}
			// Directive lines produce no code output — emit blank lines so the
			// lexer's line numbers stay aligned with the original source.
			for i := 0; i < ll.count; i++ {
				out.WriteByte('\n')
			}
		} else if active {
			out.WriteString(stripLineComment(p.expandLine(ll.text)))
			for i := 0; i < ll.count; i++ {
				out.WriteByte('\n')
			}
		} else {
			// False branch: blank lines to preserve line numbers.
			for i := 0; i < ll.count; i++ {
				out.WriteByte('\n')
			}
		}
		lineNum += ll.count
	}

	if len(conds) > 1 {
		p.errorf(file, 0, "unterminated #ifdef/#ifndef (missing #endif)")
	}
}

// parseDefine parses a #define body and registers the macro.
func (p *preprocessor) parseDefine(rest, file string, line int) {
	// Consume the macro name.
	i := 0
	for i < len(rest) && (isLetter(rest[i]) || isDigit(rest[i])) {
		i++
	}
	if i == 0 {
		p.errorf(file, line, "#define: missing or invalid macro name")
		return
	}
	name := rest[:i]
	rest = rest[i:]

	// If the immediately next character (no whitespace allowed) is '(', this
	// is a function-like macro.
	if len(rest) > 0 && rest[0] == '(' {
		rest = rest[1:] // consume '('
		close := strings.IndexByte(rest, ')')
		if close < 0 {
			p.errorf(file, line, "#define %s: missing ')' in parameter list", name)
			return
		}
		paramStr := rest[:close]
		body := stripLineComment(strings.TrimSpace(rest[close+1:]))

		var params []string
		variadic := false
		if strings.TrimSpace(paramStr) != "" {
			for _, raw := range strings.Split(paramStr, ",") {
				param := strings.TrimSpace(raw)
				if param == "..." {
					variadic = true
				} else {
					params = append(params, param)
				}
			}
		}
		if params == nil {
			params = []string{} // non-nil marks this as function-like
		}
		p.defines[name] = &macroDef{params: params, variadic: variadic, body: body}
	} else {
		body := stripLineComment(strings.TrimSpace(rest))
		p.defines[name] = &macroDef{body: body}
	}
}

// processInclude handles an #include directive.
func (p *preprocessor) processInclude(rest, file string, line int, out *strings.Builder) {
	rest = strings.TrimSpace(stripLineComment(rest))

	var filename string
	var systemSearch bool

	switch {
	case strings.HasPrefix(rest, `"`):
		end := strings.IndexByte(rest[1:], '"')
		if end < 0 {
			p.errorf(file, line, `#include: missing closing '"'`)
			return
		}
		filename = rest[1 : end+1]

	case strings.HasPrefix(rest, "<"):
		end := strings.IndexByte(rest, '>')
		if end < 0 {
			p.errorf(file, line, "#include: missing '>'")
			return
		}
		filename = rest[1:end]
		systemSearch = true

	default:
		// May be a macro; expand once and retry.
		expanded := strings.TrimSpace(p.expandLine(rest))
		if expanded == rest {
			p.errorf(file, line, "#include: invalid argument %q", rest)
			return
		}
		p.processInclude(expanded, file, line, out)
		return
	}

	// Locate the file on disk first (real files take priority over virtual headers).
	var fullPath string
	if !systemSearch {
		rel := filepath.Join(filepath.Dir(file), filename)
		if fileExists(rel) {
			fullPath = rel
		}
	}
	if fullPath == "" {
		for _, dir := range p.includePaths {
			candidate := filepath.Join(dir, filename)
			if fileExists(candidate) {
				fullPath = candidate
				break
			}
		}
	}

	if fullPath != "" {
		// Found on disk — use the real file.
	} else if content, ok := builtinHeaders[filename]; ok {
		// Fall back to virtual built-in header (e.g. when libc/ is not on the path).
		p.processFile(stripBlockComments(content), "<"+filename+">", out)
		return
	} else {
		p.errorf(file, line, "#include %q: file not found", filename)
		return
	}

	data, err := os.ReadFile(fullPath)
	if err != nil {
		p.errorf(file, line, "#include: %v", err)
		return
	}
	p.processFile(stripBlockComments(string(data)), fullPath, out)
}

// expandLine expands macros in one logical line, skipping string/char literals
// and line comments.  Multiple passes are performed until the output stabilises
// or a depth limit is reached (guards against unterminated expansion chains).
func (p *preprocessor) expandLine(line string) string {
	return p.expandLineDisabled(line, nil)
}

// expandLineDisabled expands macros in line in a single pass, skipping any
// macro names in the disabled set (blue-paint rule — prevents self-expansion).
// Each macro expansion recurses with the macro name added to disabled, so
// chains like A→B→C are handled by nesting rather than outer iteration.
func (p *preprocessor) expandLineDisabled(line string, disabled map[string]bool) string {
	return p.expandLineOnceDisabled(line, disabled)
}

func (p *preprocessor) expandLineOnce(line string) string {
	return p.expandLineOnceDisabled(line, nil)
}

func (p *preprocessor) expandLineOnceDisabled(line string, disabled map[string]bool) string {
	var out strings.Builder
	i := 0
	for i < len(line) {
		c := line[i]

		// String literal — copy verbatim.
		if c == '"' {
			j := i + 1
			for j < len(line) {
				if line[j] == '\\' {
					j += 2
					continue
				}
				if line[j] == '"' {
					j++
					break
				}
				j++
			}
			out.WriteString(line[i:j])
			i = j
			continue
		}

		// Character literal — copy verbatim.
		if c == '\'' {
			j := i + 1
			for j < len(line) {
				if line[j] == '\\' {
					j += 2
					continue
				}
				if line[j] == '\'' {
					j++
					break
				}
				j++
			}
			out.WriteString(line[i:j])
			i = j
			continue
		}

		// Line comment — copy rest verbatim (lexer handles it too).
		if c == '/' && i+1 < len(line) && line[i+1] == '/' {
			out.WriteString(line[i:])
			break
		}

		// Identifier — possibly a macro name.
		if isLetter(c) {
			j := i + 1
			for j < len(line) && (isLetter(line[j]) || isDigit(line[j])) {
				j++
			}
			name := line[i:j]
			def := p.defines[name]

			if def == nil || disabled[name] {
				// Not a macro, or disabled (blue-painted).
				out.WriteString(name)
				i = j
				continue
			}

			if def.params == nil {
				// Object-like macro: expand with this name disabled to prevent recursion.
				newDisabled := make(map[string]bool, len(disabled)+1)
				for k, v := range disabled {
					newDisabled[k] = v
				}
				newDisabled[name] = true
				expanded := p.expandLineDisabled(def.body, newDisabled)
				out.WriteString(expanded)
				i = j
				continue
			}

			// Function-like macro: scan past whitespace for '('.
			k := j
			for k < len(line) && (line[k] == ' ' || line[k] == '\t') {
				k++
			}
			if k >= len(line) || line[k] != '(' {
				// No '(' — output name unexpanded.
				out.WriteString(name)
				i = j
				continue
			}
			args, end, ok := collectArgs(line, k+1)
			if !ok {
				out.WriteString(name)
				i = j
				continue
			}
			// Do NOT pre-expand arguments before substitution; substitute the
			// raw argument text and then re-expand the whole result.  This keeps
			// the hide-set of any macro names that appear inside an argument
			// (e.g. "#define ap my_ap.ap" → va_copy(ap,…) → the expanded "ap"
			// tokens should not be re-expanded again inside the body).
			// Note: # stringification still receives the un-expanded argument, which
			// is the correct C behaviour.
			// Apply the function-like macro, then re-expand the result
			// with this name disabled (blue-paint rule).
			newDisabled := make(map[string]bool, len(disabled)+1)
			for k2, v := range disabled {
				newDisabled[k2] = v
			}
			newDisabled[name] = true
			// Pre-expand each argument that is NOT adjacent to ## in the body.
			// This follows C99 6.10.3.1: args not adjacent to # or ## are
			// fully macro-expanded before substitution.  Pre-expanding all args
			// is safe for our purposes: it makes token-pasting with macro values
			// (e.g. 1e##DTOA_DIG → 1e17) work correctly.
			expandedArgs := make([]string, len(args))
			for ai, arg := range args {
				expandedArgs[ai] = p.expandLineDisabled(arg, disabled)
				// Blue-paint rule (C99 6.10.3.4): tokens produced by expanding a
				// macro argument must not be re-expanded when the body is rescanned.
				// Since gaston tracks expansion via strings (not tokens), we
				// approximate this by disabling any macros whose names appear as
				// standalone identifiers in the *raw* argument and that caused a
				// change upon expansion.  This prevents e.g.:
				//   #define ap my_ap.ap
				//   va_copy(ap, src)  →  ((my_ap.ap)=(src))
				// from re-expanding the trailing "ap" in "my_ap.ap" during rescan.
				// Blue-paint rule: if the raw arg is a single identifier that is a
			// macro and was expanded (result differs), disable that macro during
			// the rescan so it cannot re-expand inside the substituted body.
			// We limit to single-identifier args to avoid collateral disabling of
			// unrelated macros in complex expressions.
			if expandedArgs[ai] != arg {
					trimmed := strings.TrimSpace(arg)
					if isIdentToken(trimmed) {
						if _, isMacro := p.defines[trimmed]; isMacro {
							newDisabled[trimmed] = true
						}
					}
				}
			}
			raw := p.applyFuncMacro(def, name, expandedArgs)
			expanded := p.expandLineDisabled(raw, newDisabled)
			out.WriteString(expanded)
			i = end
			continue
		}

		out.WriteByte(c)
		i++
	}
	return out.String()
}

// isIdentToken reports whether s is exactly one C identifier (no spaces or non-ident chars).
func isIdentToken(s string) bool {
	if len(s) == 0 {
		return false
	}
	for i, c := range s {
		if i == 0 {
			if c != '_' && !(c >= 'a' && c <= 'z') && !(c >= 'A' && c <= 'Z') {
				return false
			}
		} else {
			if c != '_' && !(c >= 'a' && c <= 'z') && !(c >= 'A' && c <= 'Z') && !(c >= '0' && c <= '9') {
				return false
			}
		}
	}
	return true
}

// applyFuncMacro substitutes actual arguments into a function-like macro body.
// It supports # stringification and ## token pasting.
func (p *preprocessor) applyFuncMacro(def *macroDef, name string, args []string) string {
	// Normalise: #define FOO() called as FOO() yields args=[""] but wants 0.
	if len(def.params) == 0 && len(args) == 1 && args[0] == "" {
		args = nil
	}
	if def.variadic {
		if len(args) < len(def.params) {
			return name
		}
	} else if len(args) != len(def.params) {
		return name
	}

	// paramIndex returns the index of param name in def.params, or -1.
	paramIndex := func(tok string) int {
		if tok == "__VA_ARGS__" && def.variadic {
			return len(def.params) // sentinel for variadic
		}
		for idx, param := range def.params {
			if tok == param {
				return idx
			}
		}
		return -1
	}

	// argFor returns the substituted argument string for a given param index.
	argFor := func(idx int) string {
		if idx == len(def.params) && def.variadic {
			// variadic: join extra args
			return strings.Join(args[len(def.params):], ", ")
		}
		if idx >= 0 && idx < len(args) {
			return args[idx]
		}
		return ""
	}

	var out strings.Builder
	body := def.body
	i := 0
	for i < len(body) {
		// Handle # stringification operator (not ##).
		if body[i] == '#' {
			// Peek: if next non-space is also '#', it's a paste operator — handle below.
			j := i + 1
			for j < len(body) && (body[j] == ' ' || body[j] == '\t') {
				j++
			}
			if j < len(body) && body[j] == '#' {
				// ## token-paste operator: emit both '#' chars so applyTokenPaste
				// can collapse them after argument substitution.
				out.WriteByte('#')
				out.WriteByte('#')
				i = j + 1 // skip past the second '#'
				continue
			}
			// Check if followed by an identifier (stringification).
			if j < len(body) && isLetter(body[j]) {
				k := j + 1
				for k < len(body) && (isLetter(body[k]) || isDigit(body[k])) {
					k++
				}
				tok := body[j:k]
				idx := paramIndex(tok)
				if idx >= 0 {
					arg := argFor(idx)
					// Stringify: wrap in double-quotes, escaping backslash and quote.
					escaped := strings.ReplaceAll(arg, `\`, `\\`)
					escaped = strings.ReplaceAll(escaped, `"`, `\"`)
					out.WriteByte('"')
					out.WriteString(escaped)
					out.WriteByte('"')
					i = k
					continue
				}
			}
			out.WriteByte('#')
			i++
			continue
		}

		if isLetter(body[i]) {
			j := i + 1
			for j < len(body) && (isLetter(body[j]) || isDigit(body[j])) {
				j++
			}
			tok := body[i:j]
			idx := paramIndex(tok)
			if idx >= 0 {
				out.WriteString(argFor(idx))
			} else {
				out.WriteString(tok)
			}
			i = j
			continue
		}
		out.WriteByte(body[i])
		i++
	}

	// Second pass: collapse ## token-paste operators (with optional surrounding spaces).
	result := out.String()
	result = applyTokenPaste(result)
	return result
}

// applyTokenPaste collapses ## (token-paste) operators in s.
// It handles patterns like "a ## b", "a##b", " ## b", "a ## ".
func applyTokenPaste(s string) string {
	if !strings.Contains(s, "##") {
		return s
	}
	// Repeatedly collapse leftmost ## occurrence.
	for {
		idx := strings.Index(s, "##")
		if idx < 0 {
			break
		}
		// Trim trailing spaces before ##.
		before := strings.TrimRight(s[:idx], " \t")
		// Trim leading spaces after ##.
		after := strings.TrimLeft(s[idx+2:], " \t")
		s = before + after
	}
	return s
}

// collectArgs reads macro arguments starting after the opening '(' (at position
// start in line).  It returns (args, position-after-')', ok), handling nested
// parentheses and string/char literals correctly.
func collectArgs(line string, start int) ([]string, int, bool) {
	var args []string
	var cur strings.Builder
	depth := 1
	i := start
	for i < len(line) {
		c := line[i]

		// String literal inside args — copy verbatim.
		if c == '"' {
			cur.WriteByte(c)
			i++
			for i < len(line) {
				if line[i] == '\\' {
					cur.WriteByte(line[i])
					i++
					if i < len(line) {
						cur.WriteByte(line[i])
						i++
					}
					continue
				}
				cur.WriteByte(line[i])
				if line[i] == '"' {
					i++
					break
				}
				i++
			}
			continue
		}

		// Char literal inside args — copy verbatim.
		if c == '\'' {
			cur.WriteByte(c)
			i++
			for i < len(line) {
				if line[i] == '\\' {
					cur.WriteByte(line[i])
					i++
					if i < len(line) {
						cur.WriteByte(line[i])
						i++
					}
					continue
				}
				cur.WriteByte(line[i])
				if line[i] == '\'' {
					i++
					break
				}
				i++
			}
			continue
		}

		switch c {
		case '(':
			depth++
			cur.WriteByte(c)
			i++
		case ')':
			depth--
			if depth == 0 {
				args = append(args, strings.TrimSpace(cur.String()))
				i++
				return args, i, true
			}
			cur.WriteByte(c)
			i++
		case ',':
			if depth == 1 {
				args = append(args, strings.TrimSpace(cur.String()))
				cur.Reset()
			} else {
				cur.WriteByte(c)
			}
			i++
		default:
			cur.WriteByte(c)
			i++
		}
	}
	return nil, 0, false // unclosed '('
}

// ── #if / #elif expression evaluator ─────────────────────────────────────────

// evalIfExpr evaluates a preprocessor constant expression (from #if or #elif).
func (p *preprocessor) evalIfExpr(expr, file string, line int) int64 {
	expanded := p.expandForIf(expr)
	toks := scanPPTokens(expanded)
	pos := 0
	val := evalPPExpr(toks, &pos)
	return val
}

// expandForIf expands macros in a #if expression, handles defined(), and
// replaces unknown identifiers with 0 (C standard rule).
func (p *preprocessor) expandForIf(expr string) string {
	// First, handle __has_attribute, __has_builtin, __has_feature,
	// __has_include, __has_c_attribute — replace with 0 before macro expansion.
	hasBuiltins := []string{
		"__has_attribute", "__has_builtin", "__has_feature",
		"__has_include", "__has_c_attribute", "__has_extension",
		"__has_include_next",
	}
	for _, hb := range hasBuiltins {
		for {
			idx := strings.Index(expr, hb)
			if idx < 0 {
				break
			}
			after := idx + len(hb)
			// Skip spaces.
			j := after
			for j < len(expr) && (expr[j] == ' ' || expr[j] == '\t') {
				j++
			}
			if j >= len(expr) || expr[j] != '(' {
				// Not a call — replace just the name with 0.
				expr = expr[:idx] + "0" + expr[after:]
				continue
			}
			// Find matching ')'.
			depth := 1
			k := j + 1
			for k < len(expr) && depth > 0 {
				if expr[k] == '(' {
					depth++
				} else if expr[k] == ')' {
					depth--
				}
				k++
			}
			expr = expr[:idx] + "0" + expr[k:]
		}
	}

	// Handle defined(X) and defined X.
	for {
		idx := strings.Index(expr, "defined")
		if idx < 0 {
			break
		}
		after := idx + len("defined")
		// Make sure "defined" is a complete token (not part of longer identifier).
		if idx > 0 && (isLetter(expr[idx-1]) || isDigit(expr[idx-1])) {
			// Part of a longer word — skip by replacing just the word.
			// Find end of this identifier.
			end := after
			for end < len(expr) && (isLetter(expr[end]) || isDigit(expr[end])) {
				end++
			}
			// Replace with 0 (unknown identifier).
			expr = expr[:idx] + "0" + expr[end:]
			continue
		}
		if after < len(expr) && (isLetter(expr[after]) || isDigit(expr[after])) {
			// Part of a longer word (e.g. "defined_something").
			end := after
			for end < len(expr) && (isLetter(expr[end]) || isDigit(expr[end])) {
				end++
			}
			expr = expr[:idx] + "0" + expr[end:]
			continue
		}
		// Skip spaces.
		j := after
		for j < len(expr) && (expr[j] == ' ' || expr[j] == '\t') {
			j++
		}
		if j >= len(expr) {
			expr = expr[:idx] + "0"
			break
		}
		var macroName string
		var end int
		if expr[j] == '(' {
			// defined(X) form.
			k := j + 1
			for k < len(expr) && (expr[k] == ' ' || expr[k] == '\t') {
				k++
			}
			nameStart := k
			for k < len(expr) && (isLetter(expr[k]) || isDigit(expr[k])) {
				k++
			}
			macroName = expr[nameStart:k]
			for k < len(expr) && (expr[k] == ' ' || expr[k] == '\t') {
				k++
			}
			if k < len(expr) && expr[k] == ')' {
				k++
			}
			end = k
		} else if isLetter(expr[j]) {
			// defined X form.
			k := j
			for k < len(expr) && (isLetter(expr[k]) || isDigit(expr[k])) {
				k++
			}
			macroName = expr[j:k]
			end = k
		} else {
			// Malformed defined — replace with 0.
			expr = expr[:idx] + "0" + expr[j:]
			continue
		}
		var replacement string
		if p.defines[macroName] != nil {
			replacement = "1"
		} else {
			replacement = "0"
		}
		expr = expr[:idx] + replacement + expr[end:]
	}

	// Expand remaining macros using the normal macro expander.
	expr = p.expandLine(expr)

	// Replace any remaining identifiers (undefined macros) with 0.
	// We must skip numeric literals (including hex 0x...) and character literals.
	var out strings.Builder
	i := 0
	for i < len(expr) {
		c := expr[i]

		// Skip character literals.
		if c == '\'' {
			out.WriteByte(c)
			i++
			for i < len(expr) {
				if expr[i] == '\\' {
					out.WriteByte(expr[i])
					i++
					if i < len(expr) {
						out.WriteByte(expr[i])
						i++
					}
					continue
				}
				out.WriteByte(expr[i])
				if expr[i] == '\'' {
					i++
					break
				}
				i++
			}
			continue
		}

		// Skip string literals.
		if c == '"' {
			out.WriteByte(c)
			i++
			for i < len(expr) {
				if expr[i] == '\\' {
					out.WriteByte(expr[i])
					i++
					if i < len(expr) {
						out.WriteByte(expr[i])
						i++
					}
					continue
				}
				out.WriteByte(expr[i])
				if expr[i] == '"' {
					i++
					break
				}
				i++
			}
			continue
		}

		// Skip numeric literals (decimal and hex).
		if isDigit(c) {
			j := i
			if c == '0' && j+1 < len(expr) && (expr[j+1] == 'x' || expr[j+1] == 'X') {
				j += 2
				for j < len(expr) && isHexDigit(expr[j]) {
					j++
				}
			} else {
				for j < len(expr) && isDigit(expr[j]) {
					j++
				}
			}
			// Consume integer suffixes attached to the number (e.g., 1UL, 0xFFULL).
			for j < len(expr) && (expr[j] == 'u' || expr[j] == 'U' || expr[j] == 'l' || expr[j] == 'L') {
				j++
			}
			out.WriteString(expr[i:j])
			i = j
			continue
		}

		// Replace identifiers (undefined macros) with 0.
		if isLetter(c) {
			j := i + 1
			for j < len(expr) && (isLetter(expr[j]) || isDigit(expr[j])) {
				j++
			}
			out.WriteString("0")
			i = j
			continue
		}
		out.WriteByte(c)
		i++
	}
	return out.String()
}

// ppToken is a token in a preprocessor constant expression.
type ppToken struct {
	kind string // "num", "op"
	num  int64
	op   string
}

// scanPPTokens tokenizes a preprocessor constant expression.
func scanPPTokens(s string) []ppToken {
	var toks []ppToken
	i := 0
	for i < len(s) {
		c := s[i]
		if c == ' ' || c == '\t' {
			i++
			continue
		}

		// Character literal: 'x' or '\n'.
		if c == '\'' {
			i++ // skip opening quote
			var val int64
			if i < len(s) && s[i] == '\\' {
				i++
				if i < len(s) {
					switch s[i] {
					case 'n':
						val = '\n'
					case 't':
						val = '\t'
					case 'r':
						val = '\r'
					case '0':
						val = 0
					case '\\':
						val = '\\'
					case '\'':
						val = '\''
					default:
						val = int64(s[i])
					}
					i++
				}
			} else if i < len(s) {
				val = int64(s[i])
				i++
			}
			if i < len(s) && s[i] == '\'' {
				i++ // skip closing quote
			}
			toks = append(toks, ppToken{kind: "num", num: val})
			continue
		}

		// Number.
		if isDigit(c) {
			j := i
			if c == '0' && j+1 < len(s) && (s[j+1] == 'x' || s[j+1] == 'X') {
				j += 2
				for j < len(s) && isHexDigit(s[j]) {
					j++
				}
			} else {
				for j < len(s) && isDigit(s[j]) {
					j++
				}
			}
			numStr := s[i:j]
			// Skip integer suffixes.
			for j < len(s) && (s[j] == 'u' || s[j] == 'U' || s[j] == 'l' || s[j] == 'L') {
				j++
			}
			// Parse number.
			base := 0
			v, err := strconv.ParseInt(numStr, base, 64)
			if err != nil {
				// Try unsigned.
				uv, uerr := strconv.ParseUint(numStr, base, 64)
				if uerr == nil {
					v = int64(uv)
				}
			}
			toks = append(toks, ppToken{kind: "num", num: v})
			i = j
			continue
		}

		// Two-character operators.
		if i+1 < len(s) {
			two := s[i : i+2]
			switch two {
			case "&&", "||", "==", "!=", "<=", ">=", "<<", ">>":
				toks = append(toks, ppToken{kind: "op", op: two})
				i += 2
				continue
			}
		}

		// Single-character operators.
		switch c {
		case '?', ':', '(', ')', '!', '~', '+', '-', '*', '/', '%', '&', '|', '^', '<', '>':
			toks = append(toks, ppToken{kind: "op", op: string(c)})
		default:
			// Skip unknown characters (e.g., identifiers already replaced with 0).
			if !unicode.IsSpace(rune(c)) {
				// Unknown — skip.
			}
		}
		i++
	}
	return toks
}

// evalPPExpr evaluates a preprocessor constant expression using recursive descent.
func evalPPExpr(toks []ppToken, pos *int) int64 {
	return evalTernary(toks, pos)
}

func peekOp(toks []ppToken, pos *int, op string) bool {
	if *pos < len(toks) && toks[*pos].kind == "op" && toks[*pos].op == op {
		return true
	}
	return false
}

func consumeOp(toks []ppToken, pos *int, op string) bool {
	if peekOp(toks, pos, op) {
		*pos++
		return true
	}
	return false
}

func evalTernary(toks []ppToken, pos *int) int64 {
	cond := evalOr(toks, pos)
	if consumeOp(toks, pos, "?") {
		then := evalTernary(toks, pos)
		consumeOp(toks, pos, ":")
		els := evalTernary(toks, pos)
		if cond != 0 {
			return then
		}
		return els
	}
	return cond
}

func evalOr(toks []ppToken, pos *int) int64 {
	lhs := evalAnd(toks, pos)
	for consumeOp(toks, pos, "||") {
		rhs := evalAnd(toks, pos)
		if lhs != 0 || rhs != 0 {
			lhs = 1
		} else {
			lhs = 0
		}
	}
	return lhs
}

func evalAnd(toks []ppToken, pos *int) int64 {
	lhs := evalBitOr(toks, pos)
	for consumeOp(toks, pos, "&&") {
		rhs := evalBitOr(toks, pos)
		if lhs != 0 && rhs != 0 {
			lhs = 1
		} else {
			lhs = 0
		}
	}
	return lhs
}

func evalBitOr(toks []ppToken, pos *int) int64 {
	lhs := evalBitXor(toks, pos)
	for peekOp(toks, pos, "|") {
		*pos++
		rhs := evalBitXor(toks, pos)
		lhs = lhs | rhs
	}
	return lhs
}

func evalBitXor(toks []ppToken, pos *int) int64 {
	lhs := evalBitAnd(toks, pos)
	for consumeOp(toks, pos, "^") {
		rhs := evalBitAnd(toks, pos)
		lhs = lhs ^ rhs
	}
	return lhs
}

func evalBitAnd(toks []ppToken, pos *int) int64 {
	lhs := evalEquality(toks, pos)
	for peekOp(toks, pos, "&") {
		*pos++
		rhs := evalEquality(toks, pos)
		lhs = lhs & rhs
	}
	return lhs
}

func evalEquality(toks []ppToken, pos *int) int64 {
	lhs := evalRelational(toks, pos)
	for {
		if consumeOp(toks, pos, "==") {
			rhs := evalRelational(toks, pos)
			if lhs == rhs {
				lhs = 1
			} else {
				lhs = 0
			}
		} else if consumeOp(toks, pos, "!=") {
			rhs := evalRelational(toks, pos)
			if lhs != rhs {
				lhs = 1
			} else {
				lhs = 0
			}
		} else {
			break
		}
	}
	return lhs
}

func evalRelational(toks []ppToken, pos *int) int64 {
	lhs := evalShift(toks, pos)
	for {
		if consumeOp(toks, pos, "<=") {
			rhs := evalShift(toks, pos)
			if lhs <= rhs {
				lhs = 1
			} else {
				lhs = 0
			}
		} else if consumeOp(toks, pos, ">=") {
			rhs := evalShift(toks, pos)
			if lhs >= rhs {
				lhs = 1
			} else {
				lhs = 0
			}
		} else if peekOp(toks, pos, "<") {
			*pos++
			rhs := evalShift(toks, pos)
			if lhs < rhs {
				lhs = 1
			} else {
				lhs = 0
			}
		} else if peekOp(toks, pos, ">") {
			*pos++
			rhs := evalShift(toks, pos)
			if lhs > rhs {
				lhs = 1
			} else {
				lhs = 0
			}
		} else {
			break
		}
	}
	return lhs
}

func evalShift(toks []ppToken, pos *int) int64 {
	lhs := evalAddSub(toks, pos)
	for {
		if consumeOp(toks, pos, "<<") {
			rhs := evalAddSub(toks, pos)
			lhs = lhs << uint(rhs)
		} else if consumeOp(toks, pos, ">>") {
			rhs := evalAddSub(toks, pos)
			lhs = lhs >> uint(rhs)
		} else {
			break
		}
	}
	return lhs
}

func evalAddSub(toks []ppToken, pos *int) int64 {
	lhs := evalMulDiv(toks, pos)
	for {
		if peekOp(toks, pos, "+") {
			*pos++
			rhs := evalMulDiv(toks, pos)
			lhs = lhs + rhs
		} else if peekOp(toks, pos, "-") {
			*pos++
			rhs := evalMulDiv(toks, pos)
			lhs = lhs - rhs
		} else {
			break
		}
	}
	return lhs
}

func evalMulDiv(toks []ppToken, pos *int) int64 {
	lhs := evalUnary(toks, pos)
	for {
		if peekOp(toks, pos, "*") {
			*pos++
			rhs := evalUnary(toks, pos)
			lhs = lhs * rhs
		} else if peekOp(toks, pos, "/") {
			*pos++
			rhs := evalUnary(toks, pos)
			if rhs == 0 {
				lhs = 0 // division by zero: return 0
			} else {
				lhs = lhs / rhs
			}
		} else if peekOp(toks, pos, "%") {
			*pos++
			rhs := evalUnary(toks, pos)
			if rhs == 0 {
				lhs = 0 // modulo by zero: return 0
			} else {
				lhs = lhs % rhs
			}
		} else {
			break
		}
	}
	return lhs
}

func evalUnary(toks []ppToken, pos *int) int64 {
	if consumeOp(toks, pos, "!") {
		v := evalUnary(toks, pos)
		if v == 0 {
			return 1
		}
		return 0
	}
	if consumeOp(toks, pos, "~") {
		v := evalUnary(toks, pos)
		return ^v
	}
	if peekOp(toks, pos, "-") {
		*pos++
		v := evalUnary(toks, pos)
		return -v
	}
	if peekOp(toks, pos, "+") {
		*pos++
		return evalUnary(toks, pos)
	}
	return evalPrimary(toks, pos)
}

func evalPrimary(toks []ppToken, pos *int) int64 {
	if *pos >= len(toks) {
		return 0
	}
	tok := toks[*pos]
	if tok.kind == "num" {
		*pos++
		return tok.num
	}
	if tok.kind == "op" && tok.op == "(" {
		*pos++ // consume '('
		val := evalTernary(toks, pos)
		consumeOp(toks, pos, ")")
		return val
	}
	return 0
}

// ── utility functions ────────────────────────────────────────────────────────

// splitLogical splits src into logical lines, joining \ continuations.
func splitLogical(src string) []logLine {
	raw := strings.Split(src, "\n")
	var result []logLine
	var buf strings.Builder
	count := 0
	for _, line := range raw {
		count++
		if strings.HasSuffix(line, "\\") {
			buf.WriteString(strings.TrimRight(line[:len(line)-1], " \t"))
		} else {
			buf.WriteString(line)
			result = append(result, logLine{text: buf.String(), count: count})
			buf.Reset()
			count = 0
		}
	}
	if buf.Len() > 0 || count > 0 {
		result = append(result, logLine{text: buf.String(), count: count})
	}
	return result
}

// joinOpenLines merges adjacent logical lines when a line ends with unbalanced
// open parentheses. This handles function-like macro invocations whose argument
// lists span multiple physical lines without backslash continuation, e.g.:
//
//	.xfile = FDEV_SETUP_EXT(arg1, arg2,
//	                        arg3);
//
// Counts are summed so that blank-line insertion for line-number alignment works.
func joinOpenLines(lines []logLine) []logLine {
	result := make([]logLine, 0, len(lines))
	i := 0
	for i < len(lines) {
		ll := lines[i]
		i++
		for lineParenDepth(ll.text) > 0 && i < len(lines) {
			// Never swallow a preprocessor directive line into a code line.
			if strings.HasPrefix(strings.TrimLeft(lines[i].text, " \t"), "#") {
				break
			}
			ll.text += " " + lines[i].text
			ll.count += lines[i].count
			i++
		}
		result = append(result, ll)
	}
	return result
}

// lineParenDepth returns the net unbalanced open-paren count in s, ignoring
// content inside string and character literals.
func lineParenDepth(s string) int {
	depth := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '"':
			i++
			for i < len(s) {
				if s[i] == '\\' {
					i++
				} else if s[i] == '"' {
					break
				}
				i++
			}
		case '\'':
			i++
			for i < len(s) {
				if s[i] == '\\' {
					i++
				} else if s[i] == '\'' {
					break
				}
				i++
			}
		case '(':
			depth++
		case ')':
			depth--
		}
	}
	return depth
}

// splitDirective splits "  ifdef FOO" → ("ifdef", "FOO").
func splitDirective(s string) (dir, rest string) {
	s = strings.TrimLeft(s, " \t")
	i := 0
	for i < len(s) && (isLetter(s[i]) || isDigit(s[i])) {
		i++
	}
	return s[:i], strings.TrimSpace(s[i:])
}

// firstWord returns the first whitespace-delimited token in s.
func firstWord(s string) string {
	s = strings.TrimSpace(s)
	for i := 0; i < len(s); i++ {
		if s[i] == ' ' || s[i] == '\t' {
			return s[:i]
		}
	}
	return s
}

// stripLineComment removes a trailing // comment from s, not stripping inside
// string or character literals.
func stripLineComment(s string) string {
	i := 0
	for i < len(s) {
		switch s[i] {
		case '"':
			i++
			for i < len(s) && s[i] != '"' {
				if s[i] == '\\' {
					i++
				}
				i++
			}
			if i < len(s) {
				i++
			}
		case '\'':
			i++
			for i < len(s) && s[i] != '\'' {
				if s[i] == '\\' {
					i++
				}
				i++
			}
			if i < len(s) {
				i++
			}
		case '/':
			if i+1 < len(s) && s[i+1] == '/' {
				return strings.TrimRight(s[:i], " \t")
			}
			i++
		default:
			i++
		}
	}
	return s
}

// fileExists reports whether the named path exists.
func fileExists(name string) bool {
	_, err := os.Stat(name)
	return err == nil
}
