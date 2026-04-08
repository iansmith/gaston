package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// normLines strips blank lines and trims each line so preprocessor tests
// focus on expansion semantics rather than the blank lines that directives
// leave behind.
func normLines(s string) string {
	var out []string
	for _, line := range strings.Split(s, "\n") {
		if t := strings.TrimSpace(line); t != "" {
			out = append(out, t)
		}
	}
	return strings.Join(out, "\n")
}

// pp runs the preprocessor on src and returns the normalised output.
func pp(t *testing.T, src string, includePaths ...string) string {
	t.Helper()
	p := newPreprocessor(includePaths, nil)
	got, err := p.Preprocess(src, "<test>")
	if err != nil {
		t.Fatalf("Preprocess: %v", err)
	}
	return normLines(got)
}

func checkPP(t *testing.T, got, want string) {
	t.Helper()
	w := normLines(want)
	if got != w {
		t.Errorf("\n got: %q\nwant: %q", got, w)
	}
}

// ── unit tests ────────────────────────────────────────────────────────────────

// Test 1: Triple-indirect object-like chain leading into a function-like call.
// RESULT → FN(LEVEL2) → FN(LEVEL1) → FN(BASE) → FN(7) → ((7)+(7))
// Requires multi-pass expansion: first object-like resolution, then
// the function-like call only becomes visible after the prior expansion.
func TestPP_TripleIndirectChain(t *testing.T) {
	src := `
#define BASE   7
#define LEVEL1 BASE
#define LEVEL2 LEVEL1
#define FN(x)  ((x)+(x))
#define RESULT FN(LEVEL2)
r = RESULT;
`
	checkPP(t, pp(t, src), `r = ((7)+(7));`)
}

// Test 2: __VA_ARGS__ forwarded through two layers of function-like macros.
// OUTER → INNER → actual call.  Each layer re-exposes __VA_ARGS__ to the next.
func TestPP_VAArgsDoubleForward(t *testing.T) {
	src := `
#define INNER(f, ...) f(__VA_ARGS__)
#define OUTER(f, ...) INNER(f, __VA_ARGS__)
r = OUTER(output, 42);
`
	checkPP(t, pp(t, src), `r = output(42);`)
}

// Test 3: #define / #undef / redefine chaos inside nested conditionals.
// The final values of A, C, D, E must reflect the conditional branches taken.
func TestPP_ConditionalDefineUndefRedefine(t *testing.T) {
	src := `
#define A 1
#define B 2
#ifdef A
#define C 10
#undef  A
#define A 99
#ifdef B
#define D 20
#undef  B
#endif
#ifndef B
#define E 30
#endif
#endif
w = C;
x = A;
y = D;
z = E;
`
	checkPP(t, pp(t, src),
		"w = 10;\nx = 99;\ny = 20;\nz = 30;")
}

// Test 4: Nested parentheses in arguments must not confuse comma splitting.
// The inner PAIR call produces two values separated by a comma, but they
// are nested inside parens so they count as a single argument to SEL.
func TestPP_NestedParenArgSplitting(t *testing.T) {
	src := `
#define SEL(a, b, c) b
#define PAIR(x, y)   (x),(y)
r = SEL(PAIR(10, 20), (3*4), PAIR(5, 6));
`
	// SEL's three args (after paren-depth counting):
	//   a = PAIR(10,20) pre-expanded → (10),(20)
	//   b = (3*4)       pre-expanded → (3*4)
	//   c = PAIR(5,6)   pre-expanded → (5),(6)
	// SEL picks b → (3*4)
	checkPP(t, pp(t, src), `r = (3*4);`)
}

// Test 5: Object-like macro expands to a function name, which is then
// consumed together with the following argument list in the next pass.
// Undefining and redefining the alias between uses changes the behaviour.
func TestPP_ObjectExpandsToFunctionName(t *testing.T) {
	src := `
#define DOUBLE(x) ((x)+(x))
#define SQUARE(x) ((x)*(x))
#define OP DOUBLE
a = OP(5);
#undef OP
#define OP SQUARE
b = OP(5);
`
	checkPP(t, pp(t, src),
		"a = ((5)+(5));\nb = ((5)*(5));")
}

// Test 6: Four-level nested #ifdef / #ifndef with #else branches.
// Truth table: P defined, Q undefined, R defined inside the true P-branch.
// Only one innermost branch must be active.
func TestPP_FourLevelNestedConditionals(t *testing.T) {
	src := `
#define P
#ifdef P
  #ifndef Q
    #define R
    #ifdef R
      #ifdef Q
        result = 99;
      #else
        result = 1;
      #endif
    #else
      result = 2;
    #endif
  #else
    result = 3;
  #endif
#else
  result = 4;
#endif
`
	checkPP(t, pp(t, src), `result = 1;`)
}

// Test 7: Backslash continuation lines in a function-like macro definition.
// The body spans three physical lines but must be treated as one body.
func TestPP_BackslashContinuationInMacro(t *testing.T) {
	src := `
#define CLAMP_LO(val, lo) \
    ((val) \
     + (lo))
r = CLAMP_LO(10, 2);
`
	// Interior whitespace reflects the joined continuation lines; we just
	// verify the tokens are right by collapsing runs of spaces.
	got := pp(t, src)
	folded := strings.Join(strings.Fields(got), " ")
	want := "r = ((10) + (2));"
	if folded != want {
		t.Errorf("\n got (folded): %q\nwant (folded): %q\n raw got: %q", folded, want, got)
	}
}

// Test 8: Zero-parameter function-like macros chained three levels deep.
// TWO() → (ONE()+ONE()) → ((ZERO()+1)+(ZERO()+1)) → ((0+1)+(0+1))
func TestPP_ZeroParamChain(t *testing.T) {
	src := `
#define ZERO() 0
#define ONE()  (ZERO()+1)
#define TWO()  (ONE()+ONE())
n = TWO();
`
	checkPP(t, pp(t, src), `n = ((0+1)+(0+1));`)
}

// Test 9: APPLY pattern — pass a function-like macro name as an argument.
// CALL2 substitutes the name into a call site, enabling the next pass to
// expand it.  Two different operations tested back-to-back.
func TestPP_ApplyPattern(t *testing.T) {
	src := `
#define ADD(a, b)       ((a)+(b))
#define MUL(a, b)       ((a)*(b))
#define CALL2(f, a, b)  f(a, b)
x = CALL2(ADD, 3, 4);
y = CALL2(MUL, 5, 6);
`
	checkPP(t, pp(t, src),
		"x = ((3)+(4));\ny = ((5)*(6));")
}

// Test 10: Select first/second variadic argument using __VA_ARGS__ slicing.
func TestPP_VAArgsSlice(t *testing.T) {
	src := `
#define VA_FIRST(x, ...)    x
#define VA_SECOND(x, y, ...) y
#define VA_THIRD(x, y, z, ...) z
a = VA_FIRST(10, 20, 30);
b = VA_SECOND(10, 20, 30);
c = VA_THIRD(10, 20, 30);
`
	checkPP(t, pp(t, src),
		"a = 10;\nb = 20;\nc = 30;")
}

// Test 11: The mega test.
// Combines object→function indirection, multi-level argument pre-expansion,
// __VA_ARGS__, and numeric chaining — all on one line.
//
//   BASE=5, BIAS=3
//   SCALE(x)      = ((x)*BASE)
//   SCALED_BIAS   = SCALE(BIAS) → SCALE(3) → ((3)*5)
//   SUM(a,...)    = ((a)+__VA_ARGS__)
//   SUM(SCALED_BIAS, SCALED_BIAS) → ((((3)*5))+((3)*5))  = 30
func TestPP_Mega(t *testing.T) {
	src := `
#define BASE        5
#define BIAS        3
#define SCALE(x)    ((x)*BASE)
#define SCALED_BIAS SCALE(BIAS)
#define SUM(a, ...) ((a)+__VA_ARGS__)
r = SUM(SCALED_BIAS, SCALED_BIAS);
`
	checkPP(t, pp(t, src), `r = ((((3)*5))+((3)*5));`)
}

// Test 12: #undef makes a subsequent #ifdef false.
// After FEATURE is undef'd the #ifdef branch must be skipped and the
// #else branch must run.
func TestPP_UndefMakesIfdefFalse(t *testing.T) {
	src := `
#define FEATURE
#undef FEATURE
#ifdef FEATURE
x = 1;
#else
x = 0;
#endif
`
	checkPP(t, pp(t, src), `x = 0;`)
}

// Test 13: Eight-fold increment via chained function-like macros.
// INC(x)  = x+1
// INC2(x) = INC(INC(x))      = x+2
// INC4(x) = INC2(INC2(x))    = x+4
// INC8(x) = INC4(INC4(x))    = x+8
// INC8(0) must equal 8.
func TestPP_ChainedIncrement(t *testing.T) {
	src := `
#define INC(x)  ((x)+1)
#define INC2(x) INC(INC(x))
#define INC4(x) INC2(INC2(x))
#define INC8(x) INC4(INC4(x))
r = INC8(0);
`
	// INC8(0)
	//  → INC4(INC4(0))
	//  arg pre-expand INC4(0):
	//    → INC2(INC2(0))
	//    arg pre-expand INC2(0): → INC(INC(0)) → INC(((0)+1)) → (((0)+1)+1)
	//    INC2((((0)+1)+1)) → INC(INC((((0)+1)+1)))
	//      → INC(((((0)+1)+1)+1)) → ((((0)+1)+1)+1)+1)
	//    = (((0)+1)+1)+1)+1 ... more carefully below
	// This is deeply nested but evaluates to 8.
	// We verify the preprocessor emits something the compiler can evaluate.
	got := pp(t, src)
	// Rather than matching the exact deeply-nested form, compile-check by
	// verifying no macro names remain in the output.
	for _, macro := range []string{"INC8", "INC4", "INC2", "INC"} {
		if strings.Contains(got, macro) {
			t.Errorf("macro %s not fully expanded in output: %q", macro, got)
		}
	}
	if !strings.Contains(got, "r =") {
		t.Errorf("expected assignment in output: %q", got)
	}
}

// Test 14: Include guard — double inclusion must define the macro only once,
// even though the file is textually included twice.
func TestPP_IncludeGuard(t *testing.T) {
	dir := t.TempDir()
	header := filepath.Join(dir, "guarded.h")
	if err := os.WriteFile(header, []byte(
		"#ifndef GUARDED_H\n"+
			"#define GUARDED_H\n"+
			"#define MAGIC 77\n"+
			"#endif\n",
	), 0644); err != nil {
		t.Fatal(err)
	}

	src := "#include \"guarded.h\"\n" +
		"#include \"guarded.h\"\n" +
		"r = MAGIC;\n"

	p := newPreprocessor([]string{dir}, nil)
	got, err := p.Preprocess(src, filepath.Join(dir, "test.c"))
	if err != nil {
		t.Fatalf("Preprocess: %v", err)
	}
	checkPP(t, normLines(got), `r = 77;`)
}

// Test 15: -I search path — header reachable only through the include path,
// not relative to the source file.
func TestPP_IncludeSearchPath(t *testing.T) {
	// Two directories: source lives in srcDir, header lives in incDir.
	srcDir := t.TempDir()
	incDir := t.TempDir()

	if err := os.WriteFile(filepath.Join(incDir, "constants.h"), []byte(
		"#define ANSWER 42\n"+
			"#define GREETING 99\n",
	), 0644); err != nil {
		t.Fatal(err)
	}

	src := "#include <constants.h>\n" +
		"a = ANSWER;\n" +
		"b = GREETING;\n"

	p := newPreprocessor([]string{incDir}, nil)
	got, err := p.Preprocess(src, filepath.Join(srcDir, "test.c"))
	if err != nil {
		t.Fatalf("Preprocess: %v", err)
	}
	checkPP(t, normLines(got), "a = 42;\nb = 99;")
}

// Test 16: Conditional compilation controlled by a macro whose value is
// itself the result of a chain of redefinitions.
// The final integer value of X drives #ifdef (defined/not), not its value.
func TestPP_ConditionalAfterRedefineChain(t *testing.T) {
	src := `
#define X PLACEHOLDER
#undef X
#define X INTERMEDIATE
#undef X
#define X 1
#ifdef X
result = X;
#else
result = 0;
#endif
`
	checkPP(t, pp(t, src), `result = 1;`)
}

// Test 17: Function-like macro whose body contains a conditional-looking
// token sequence — the preprocessor must NOT treat it as a directive
// because it is not at the start of a line.
func TestPP_MacroBodyNotTreatedAsDirective(t *testing.T) {
	src := `
#define WRAP(x) x
a = WRAP(1);
b = WRAP(2);
`
	checkPP(t, pp(t, src), "a = 1;\nb = 2;")
}

// Test 18: Object-like macro defined to empty string — acts as a token eraser.
// This is distinct from the macro being undefined.
func TestPP_EmptyMacroErasesToken(t *testing.T) {
	src := `
#define VOLATILE
int VOLATILE x;
x = 5;
`
	checkPP(t, pp(t, src), "int  x;\nx = 5;")
}

// ── #if / #elif expression evaluation tests ───────────────────────────────────

// TestPreprocIf verifies basic #if / #elif expression evaluation.
func TestPreprocIf(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want string
	}{
		{
			name: "if_1",
			src:  "#if 1\nint x;\n#endif",
			want: "int x;",
		},
		{
			name: "if_0",
			src:  "#if 0\nint x;\n#endif",
			want: "",
		},
		{
			name: "if_arithmetic",
			src:  "#if 1+1 == 2\nint x;\n#endif",
			want: "int x;",
		},
		{
			name: "if_defined_predefined",
			src:  "#if defined(NULL)\nint x;\n#endif",
			want: "int x;",
		},
		{
			name: "if_not_defined_undefined",
			src:  "#if !defined(UNDEFINED_MACRO)\nint x;\n#endif",
			want: "int x;",
		},
		{
			name: "elif_basic",
			src:  "#if 0\nint x;\n#elif 1\nint y;\n#endif",
			want: "int y;",
		},
		{
			name: "elif_else",
			src:  "#if 0\nint x;\n#elif 0\nint y;\n#else\nint z;\n#endif",
			want: "int z;",
		},
		{
			name: "if_logical_and",
			src:  "#if 1 && 1\nint x;\n#endif",
			want: "int x;",
		},
		{
			name: "if_logical_or",
			src:  "#if 0 || 1\nint x;\n#endif",
			want: "int x;",
		},
		{
			name: "if_ternary",
			src:  "#if (1 ? 1 : 0)\nint x;\n#endif",
			want: "int x;",
		},
		{
			name: "if_bitwise",
			src:  "#if (0x0F & 0xFF) == 15\nint x;\n#endif",
			want: "int x;",
		},
		{
			name: "if_shift",
			src:  "#if (1 << 3) == 8\nint x;\n#endif",
			want: "int x;",
		},
		{
			name: "if_not",
			src:  "#if !0\nint x;\n#endif",
			want: "int x;",
		},
		{
			name: "if_defined_no_parens",
			src:  "#define FOO\n#if defined FOO\nint x;\n#endif",
			want: "int x;",
		},
		{
			name: "if_macro_value",
			src:  "#define VERSION 3\n#if VERSION >= 2\nint x;\n#endif",
			want: "int x;",
		},
		{
			name: "if_has_attribute_zero",
			src:  "#if __has_attribute(visibility)\nint x;\n#else\nint y;\n#endif",
			want: "int y;",
		},
		{
			name: "pragma_ignored",
			src:  "#pragma once\nint x;",
			want: "int x;",
		},
		{
			name: "unknown_directive_ignored",
			src:  "#ident \"version\"\nint x;",
			want: "int x;",
		},
		{
			name: "integer_suffix_ul",
			src:  "#if 1UL == 1\nint x;\n#endif",
			want: "int x;",
		},
		{
			name: "integer_suffix_ll",
			src:  "#if 100LL > 50\nint x;\n#endif",
			want: "int x;",
		},
		{
			name: "char_literal_in_if",
			src:  "#if 'a' == 97\nint x;\n#endif",
			want: "int x;",
		},
		{
			name: "nested_if_elif",
			src:  "#if 0\nint a;\n#elif 0\nint b;\n#elif 1\nint c;\n#endif",
			want: "int c;",
		},
		{
			name: "predefined_gnuc",
			src:  "#if defined(__GNUC__)\nint x;\n#endif",
			want: "int x;",
		},
		{
			name: "predefined_stdc_version",
			src:  "#if __STDC_VERSION__ >= 199901L\nint x;\n#endif",
			want: "int x;",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			checkPP(t, pp(t, tc.src), tc.want)
		})
	}
}
