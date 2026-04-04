package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// dockerTest describes one end-to-end test: compile a .cm file, run the
// resulting ARM64 ELF in an Alpine container, check stdout.
type dockerTest struct {
	name  string // base name (testdata/<name>.cm → /tmp/gaston-test-<name>)
	stdin string // bytes piped to the program's stdin (usually empty)
	want  string // expected exact stdout
}

var featureTests = []dockerTest{
	// ── Feature 1: print_char / print_string ─────────────────────────────
	{name: "pc_literal", want: "Hello\n"},
	{name: "pc_var", want: "ABCDE\n"},
	{name: "ps_basic", want: "hello\n"},
	{name: "ps_multi", want: "one\ntwo\nthree\n"},

	// ── Feature 2: multiple declarations ─────────────────────────────────
	{name: "multi_local", want: "30\n"},
	{name: "multi_global", want: "100\n200\n300\n"},
	{name: "multi_three", want: "12\n3\n15\n"},

	// ── Feature 3: const ─────────────────────────────────────────────────
	{name: "const_global", want: "100\n5\n95\n"},
	{name: "const_local", want: "10\n7\n70\n"},
	{name: "const_loop", want: "15\n"},
	{name: "const_expr", want: "1\n0\n20\n"},

	// ── Feature 4: char type and literals ────────────────────────────────
	{name: "char_literal", want: "Hi\n"},
	{name: "char_arith", want: "abcde\n"},
	{name: "char_var", want: "ABCDE\n"},
	{name: "str_basic", want: "hello world\nsecond line\n"},
	{name: "str_escape", want: "tab:\there\nslash: \\\n"},

	// ── Feature 5: pointers ──────────────────────────────────────────────
	{name: "ptr_basic", want: "42\n99\n"},
	{name: "ptr_param", want: "12\n"},
	{name: "ptr_swap", want: "3\n7\n"},
	{name: "ptr_array", want: "10\n20\n30\n"},
	{name: "ptr_global", want: "42\n"},

	// ── Feature 6: malloc/free ───────────────────────────────────────────
	{name: "malloc_basic", want: "0\n1\n4\n9\n16\n25\n36\n49\n64\n81\n"},
	{name: "malloc_local", want: "1\n9\n25\n"},
	{name: "malloc_two", want: "11\n22\n33\n44\n55\n"},
	{name: "malloc_func", want: "0\n2\n4\n6\n8\n10\n"},
	{name: "malloc_reuse", want: "1\n30\n"},
	{name: "malloc_large", want: "5050\n"},
	{name: "malloc_modify", want: "12\n22\n32\n"},

	// ── Feature 7: long / long long ──────────────────────────────────────
	{name: "long_basic", want: "3000000\n"},
	{name: "long_types", want: "1000000000\n2000000000\n3000000000\n4000000000\n3000000000\n"},

	// ── Feature 8: unsigned int / unsigned long ───────────────────────────
	// unsigned_div: 100/7=14, 100%7=2, UINT_MAX/2>0→1, UINT_MAX%2=1→1
	{name: "unsigned_div", want: "14\n2\n1\n1\n"},
	// unsigned_cmp: big(-1 unsigned) vs small(1): >, >=, <, <= all true; ==, != correct
	{name: "unsigned_cmp", want: "1\n1\n1\n1\n1\n1\n"},
	// unsigned_shr: (-8)>>62 = 3; (-4)>>1 > 0 unsigned → 1
	{name: "unsigned_shr", want: "3\n1\n"},
	// unsigned_arith: 13,7,30,4, then compound: 15,12,24,6,2, then ul: 2000000000
	{name: "unsigned_arith", want: "13\n7\n30\n4\n15\n12\n24\n6\n2\n2000000000\n"},

	// ── Feature 9: short / unsigned short ────────────────────────────────
	{name: "short_basic", want: "3000\n3000\n"},
	// short_types: 1000, 20000, 100, compound: 150,50, unsigned short: 700
	{name: "short_types", want: "1000\n20000\n100\n150\n50\n700\n"},

	// ── Feature 10: float / double ───────────────────────────────────────
	// float_basic: literal, assignment, int conversion
	{name: "float_basic", want: "1\n3\n2\n1\n"},
	// float_arith: +, -, *, / with exact binary fractions
	{name: "float_arith", want: "5\n2\n6\n3\n3\n"},
	// float_cmp: <, <=, >, >=, ==, != operators
	{name: "float_cmp", want: "1\n1\n0\n0\n0\n1\n1\n0\n"},
	// float_neg: unary negation
	{name: "float_neg", want: "3\n1\n7\n"},
	// float_conv: double→int (truncation) and int→double
	{name: "float_conv", want: "7\n15\n3\n-2\n"},
	// float_global: global double variables
	{name: "float_global", want: "10\n5\n18\n8\n"},
	// float_loop: accumulation and multiplication in loops
	{name: "float_loop", want: "4\n16\n"},
	// float_if: FP comparisons controlling if/while
	{name: "float_if", want: "1\n0\n1\n3\n"},
	// float_func: double function parameters and return values
	{name: "float_func", want: "6\n2\n5\n4\n"},
	// float_print: print_double runtime (integer-valued and fractional doubles)
	{name: "float_print", want: "3.000000\n0.500000\n-1.250000\n100.000000\n"},

	// ── Feature: goto / labeled statements ──────────────────────────────
	// goto_basic: loop with goto, outputs 0–4 then 99
	{name: "goto_basic", want: "0\n1\n2\n3\n4\n99\n"},

	// ── Feature: variable-length arrays (VLAs) ───────────────────────────
	// vla_basic: int[n] with runtime n=5 → sum(0+2+4+6+8)=20; n=4 → sum(0+2+4+6)=12
	{name: "vla_basic", want: "20\n12\n"},
	// vla_param: VLA inside function; dot-product: 3→14 (1+4+9), 4→30 (1+4+9+16)
	{name: "vla_param", want: "14\n30\n"},

	// ── Integration ──────────────────────────────────────────────────────
	{name: "combo_all", want: "63\nABC\nok\n"},

	// ── Feature 11: structs ──────────────────────────────────────────────
	// struct_basic: local struct, assign and read fields
	{name: "struct_basic", want: "10\n20\n30\n"},
	// struct_short_field: struct with short field; verifies layout (short@0, int@8, sizeof=16)
	{name: "struct_short_field", want: "1000\n42\n16\n32767\n9999\n"},
	// struct_float_field: struct with float field (item 1+2); float@0, int@8, sizeof=16
	{name: "struct_float_field", want: "3.500000\n100\n16\n9.750000\n100\n"},
	// struct_ptr: pointer to struct, -> access, pass to function
	{name: "struct_ptr", want: "3\n7\n10\n"},
	// struct_global: global struct variable, function modifies via . access
	{name: "struct_global", want: "3\n60\n"},
	// struct_nested: 4-field struct, larger offsets, pass by pointer to function
	{name: "struct_nested", want: "1\n2\n10\n20\n200\n"},
	// struct_char_field: char+int struct; verifies ABI-aligned layout (char@0, int@8, size=16)
	{name: "struct_char_field", want: "65\n42\n16\n"},

	// ── Feature 12: variadic functions ───────────────────────────────────
	// variadic_basic: variadic sum of N integer args
	{name: "variadic_basic", want: "60\n100\n10\n"},
	// variadic_ptr: variadic function reading string pointer args
	{name: "variadic_ptr", want: "hello\nworld\ndone\n"},

	// ── Feature 13: pointer arithmetic / void* / double pointers ─────────
	// ptr_double: store and load double values via double* pointer
	{name: "ptr_double", want: "3.000000\n7.000000\n7.000000\n"},
	// ptr_float: store and load float values via float* pointer (item 1: TypeFloatPtr)
	{name: "ptr_float", want: "2.500000\n7.250000\n7.250000\n"},
	// ptr_arith: p+n auto-scales by 8; p-1 retreats one element
	{name: "ptr_arith", want: "10\n20\n30\n40\n30\n20\n"},
	// ptr_inc: p++/p--/p+=/p-= advance/retreat by element size
	{name: "ptr_inc", want: "10\n20\n40\n30\n10\n"},
	// ptr_void: void* accepts int* in assignment; malloc returns void*
	{name: "ptr_void", want: "0\n2\n4\n"},
	// ptr_ptr: int** double pointer dereference and assignment through
	{name: "ptr_ptr", want: "42\n99\n99\n"},
	// ptr_ptr_arr: int** subscript on malloc'd array of pointers
	{name: "ptr_ptr_arr", want: "10\n20\n30\n20\n"},
	// char_ptr_ptr: char** array of string pointers
	{name: "char_ptr_ptr", want: "alpha\nbeta\ngamma\n"},

	// ── Feature 14: pointer comparisons ──────────────────────────────────
	// ptr_cmp: null check and same-type ordering comparisons
	{name: "ptr_cmp", want: "0\n1\n0\n1\n"},

	// ── Feature 15: sizeof operator ──────────────────────────────────────
	// sizeof_basic: sizeof(type), sizeof(expr), sizeof(struct)
	{name: "sizeof_basic", want: "8\n1\n8\n1\n8\n16\n16\n"},
	// sizeof_array: sizeof(local_arr)=N×8, sizeof(arr_param)=8, sizeof(global_arr)=N×8
	{name: "sizeof_array", want: "24\n40\n8\n"},
	// sizeof_types: sizeof for float=4, double=8, short=2, unsigned short=2, unsigned char=1, int=8, char=1
	{name: "sizeof_types", want: "4\n8\n2\n2\n1\n8\n1\n"},

	// ── Feature 16: struct-by-value fields ───────────────────────────────
	// struct_value: nested struct fields; chained dot access; recursive SizeBytes
	{name: "struct_value", want: "1\n2\n10\n20\n12\n16\n32\n"},

	// ── Feature 17: pointer assignment type checking ──────────────────────
	// ptr_compat: void*↔any pointer, same-type, and null constant are all valid
	{name: "ptr_compat", want: "99\n99\n"},

	// ── Feature 18: integer promotion (char/short → int before arithmetic) ─
	// int_promote: signed/unsigned char and short overflow, compound assign
	{name: "int_promote", want: "-128\n-56\n-55\n200\n0\n-32768\n0\n"},
	// int_promote_arith: cross-type char+short arithmetic; no intermediate overflow
	{name: "int_promote_arith", want: "254\n-2\n30000\n30000\n0\n"},

	// ── Item 9: enum / union / typedef / function pointers / const* ──────────
	// enum_basic: enum constants auto-increment from 0; explicit value restarts counter
	{name: "enum_basic", want: "0\n1\n2\n10\n11\n12\n"},
	// union_basic: all fields share offset 0; sizeof = max field size rounded to alignment
	{name: "union_basic", want: "1094861636\n65\n8\n"},
	// typedef_basic: typedef creates an alias; variables declared with typedef'd name
	{name: "typedef_basic", want: "42\n50\n"},
	// funcptr_basic: function pointer assign and call
	{name: "funcptr_basic", want: "7\n12\n30\n"},

	// ── Integration tests: features used in combination ───────────────────
	// deep_struct: 5-level nested struct; chained dot access; sizeof at each level
	{name: "deep_struct", want: "1\n2\n3\n4\n5\n8\n16\n24\n32\n40\n"},
	// union_in_struct: struct↔union alternating nesting; aliasing; sizeof
	{name: "union_in_struct", want: "7\n3\n4\n3\n16\n16\n24\n24\n"},
	// enum_flags: enum bit flags combined with bitwise ops
	{name: "enum_flags", want: "5\n1\n0\n4\n7\n6\n2\n1\n"},
	// typedef_funcptr_param: typedef'd func ptr as local, global, and function parameter
	{name: "typedef_funcptr_param", want: "7\n12\n15\n5\n25\n"},
	// const_ptr_alias: const* aliasing; modify via writable alias; re-point const*
	{name: "const_ptr_alias", want: "42\n99\n99\n100\n10\n30\n"},
	// enum_union_dispatch: all 5 new features together — tagged union with enum discriminant,
	// typedef'd func ptr as callback parameter
	{name: "enum_union_dispatch", want: "42\n3.140000\n7\n99\n"},

	// ── Byzantine stress tests (one per type-system gap item) ─────────────────────
	// Item 2: struct with char+short in same 8-byte window; char@0, short@2 → byte/halfword stores
	{name: "struct_mixed_fields", want: "32\n65\n1000\n999999\n2.500000\n77\n"},
	// Item 3: 3-level mixed -> . . chains; double field via IRFFieldLoad at depth 3
	{name: "deep_arrow_dot", want: "42\n1.500000\n99\n7\n16\n24\n32\n"},
	// Item 6: sizeof used in arithmetic, as divisor for element count, in comparisons
	{name: "sizeof_exprs", want: "40\n8\n5\n15\n1\n0\n"},
	// Item 7: void* round-trip through three functions; int* and double* round-trips
	{name: "void_ptr_chain", want: "42\n99\n7.000000\n"},
	// Item 8: char+char → int (no overflow); stored back to char (wraps); short overflow
	{name: "promo_wrap", want: "200\n-56\n10000\n-32768\n"},
	// Item 9a: function pointer as struct field; "vtable" dispatch through pointer
	{name: "vtable", want: "3\n8\n15\n0\n"},
	// Item 9b: enum constants as array indices, in arithmetic, mixed with sizeof
	{name: "enum_arith", want: "3\n1\n0\n30\n97\n3\n"},
	// Item 9c: const* aliasing, re-seat, passed to function
	{name: "const_ptr_write", want: "10\n20\n20\n30\n"},
	// Item 13: VLA filled in loop, passed to function; two different sizes
	{name: "vla_sum", want: "55\n15\n8\n"},
	// Items 1+4: double* pointer arithmetic, *(p+k) reads at stride 8
	{name: "double_ptr_ops", want: "1.000000\n3.000000\n6.000000\n9.000000\n"},
	// Items 4+12: char** as pointer table; deref through double indirection
	{name: "charpp_table", want: "65\n66\n90\n"},
	// Items 2+3+9 combined: union with double inside nested struct, 3-level dot chain
	{name: "union_float_chain", want: "42\n3.140000\n42\n24\n"},
	// Item 5 (documents current non-standard behavior): sizeof(int)=8 in gaston
	{name: "sizeof_int_abi", want: "8\n8\n16\n"},

	// ── Item 4: double** and float** pointer-to-pointer types ─────────────
	{name: "dbl_ptr_ptr", want: "3.140000\n2.718000\n"},
	// ── Item 11: 2D arrays ────────────────────────────────────────────────
	{name: "multi_dim", want: "66\n0\n11\n"},
	// ── Item 12: arrays of pointers ───────────────────────────────────────
	{name: "ptr_arr", want: "10\n20\n30\n99\n"},
	// ── Item 14: _Bool / bool type ────────────────────────────────────────
	{name: "bool_basic", want: "1\n0\n1\n1\n"},
	// ── Item 15: bit-fields ───────────────────────────────────────────────
	{name: "bitfield_basic", want: "5\n17\n100\n8\n"},
	// ── Item 16: flexible array members ──────────────────────────────────
	{name: "flex_array", want: "4\n100\n8\n"},
	// ── Item 17: static local variables ──────────────────────────────────
	{name: "static_local", want: "1\n2\n3\n"},
	// ── Items 17+18: register/volatile as no-ops ─────────────────────────
	{name: "register_volatile", want: "42\n43\n"},
}

// sepTest describes a separate-compilation test: compile multiple .cm files
// to .o, link them, run the result in Docker.
type sepTest struct {
	name  string   // test name (for /tmp paths and expected-output file)
	files []string // testdata/<file>.cm sources (first must contain main)
	want  string   // expected exact stdout
}

var sepTests = []sepTest{
	// two files: extern functions (add, mul)
	{name: "sep_basic", files: []string{"sep_main", "sep_lib"}, want: "7\n12\n"},
	// extern global variable shared between files
	{name: "sep_globals", files: []string{"sep_globals_main", "sep_globals_lib"}, want: "3\n"},
	// three-file call chain: main → compute → double_it
	{name: "sep_chain", files: []string{"sep_chain_main", "sep_chain_a", "sep_chain_b"}, want: "11\n"},
	// mutual recursion across two files (is_odd ↔ is_even)
	{name: "sep_mutual", files: []string{"sep_mutual_a", "sep_mutual_b"}, want: "1\n0\n"},
	// pointer parameter crosses file boundary; malloc used in main file
	{name: "sep_ptr", files: []string{"sep_ptr_main", "sep_ptr_lib"}, want: "2\n4\n6\n8\n10\n"},
}

// compileObj compiles testdata/<name>.cm to an ET_REL object at outPath.
func compileObj(name, outPath string) error {
	srcPath := fmt.Sprintf("testdata/%s.cm", name)
	raw, err := os.ReadFile(srcPath)
	if err != nil {
		return fmt.Errorf("read %s: %w", srcPath, err)
	}
	pp := newPreprocessor(nil)
	src, err := pp.Preprocess(string(raw), srcPath)
	if err != nil {
		return fmt.Errorf("preprocess %s: %w", srcPath, err)
	}
	lex := newLexer(src, srcPath)
	yyParse(lex)
	if lex.errors > 0 {
		return fmt.Errorf("%s: %d parse error(s)", name, lex.errors)
	}
	if lex.result == nil {
		return fmt.Errorf("%s: empty program", name)
	}
	// requireMain=false: library files don't need main
	if err := semCheck(lex.result, false); err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	irp := genIR(lex.result)
	return genObjectFile(irp, outPath)
}

// TestSepCompile compiles multiple .cm files to .o, links them, and runs the
// result in an Alpine ARM64 container.
func TestSepCompile(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker not found in PATH; skipping container tests")
	}

	for _, tt := range sepTests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			var objPaths []string
			for _, f := range tt.files {
				obj := fmt.Sprintf("/tmp/gaston-test-%s-%s.o", tt.name, f)
				objPaths = append(objPaths, obj)
				t.Cleanup(func() { os.Remove(obj) })
				if err := compileObj(f, obj); err != nil {
					t.Fatalf("compile %s: %v", f, err)
				}
			}

			binPath := fmt.Sprintf("/tmp/gaston-test-%s", tt.name)
			t.Cleanup(func() { os.Remove(binPath) })
			if err := link(binPath, objPaths); err != nil {
				t.Fatalf("link: %v", err)
			}

			cmd := exec.Command("docker", "run", "--rm",
				"--platform", "linux/arm64",
				"-i",
				"-v", binPath+":/prog",
				"alpine:latest",
				"/prog",
			)
			out, err := cmd.Output()
			if err != nil {
				if ee, ok := err.(*exec.ExitError); ok {
					t.Fatalf("docker run failed (exit %d):\nstderr: %s",
						ee.ExitCode(), string(ee.Stderr))
				}
				t.Fatalf("docker run: %v", err)
			}

			got := string(out)
			if got != tt.want {
				t.Errorf("output mismatch:\n  got  %q\n  want %q", got, tt.want)
			}
		})
	}
}

// compileTest compiles testdata/<name>.cm to an ARM64 ELF at outPath using
// gaston's internal pipeline (no subprocess).
func compileTest(name, outPath string) error {
	srcPath := fmt.Sprintf("testdata/%s.cm", name)
	raw, err := os.ReadFile(srcPath)
	if err != nil {
		return fmt.Errorf("read %s: %w", srcPath, err)
	}
	pp := newPreprocessor(nil)
	src, err := pp.Preprocess(string(raw), srcPath)
	if err != nil {
		return fmt.Errorf("preprocess %s: %w", srcPath, err)
	}
	lex := newLexer(src, srcPath)
	yyParse(lex)
	if lex.errors > 0 {
		return fmt.Errorf("%s: %d parse error(s)", name, lex.errors)
	}
	if lex.result == nil {
		return fmt.Errorf("%s: empty program", name)
	}
	if err := semCheck(lex.result, true); err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	irp := genIR(lex.result)
	return genELF(irp, outPath)
}

// compileObjPath compiles a .cm file at srcPath to an ET_REL object at outPath,
// using the given include search paths.
func compileObjPath(srcPath, outPath string, includePaths []string) error {
	raw, err := os.ReadFile(srcPath)
	if err != nil {
		return fmt.Errorf("read %s: %w", srcPath, err)
	}
	pp := newPreprocessor(includePaths)
	src, err := pp.Preprocess(string(raw), srcPath)
	if err != nil {
		return fmt.Errorf("preprocess %s: %w", srcPath, err)
	}
	lex := newLexer(src, srcPath)
	yyParse(lex)
	if lex.errors > 0 {
		return fmt.Errorf("%s: %d parse error(s)", srcPath, lex.errors)
	}
	if lex.result == nil {
		return fmt.Errorf("%s: empty program", srcPath)
	}
	if err := semCheck(lex.result, false); err != nil {
		return fmt.Errorf("%s: %w", srcPath, err)
	}
	irp := genIR(lex.result)
	return genObjectFile(irp, outPath)
}

// libcTest describes a test that links against the gaston libc (libc/stdio.cm).
type libcTest struct {
	name string // testdata/<name>.cm is the main program
	want string // expected stdout
}

var libcTests = []libcTest{
	// ── Feature 13: libc printf / puts / putchar ──────────────────────────
	{name: "hello_world", want: "Hello, world!\n"},
	{name: "printf_fmt",  want: "count=42\nstr=hello!\nchar=A\n3+4=7\n"},
	{name: "puts_test",   want: "one\ntwo\nthree\n"},
	// ── Feature 14: libc sscanf ───────────────────────────────────────────
	{name: "sscanf_basic", want: "n=42 r=1\ns=hello r=1\na=-7 b=99 r=2\nc=X r=1\n"},
}

// buildLibgastonc compiles libc/stdio.cm to stdio.o, then archives it into
// libgastonc.a, returning the archive path.  The caller must clean up both.
func buildLibgastonc(t *testing.T) (libPath, objPath string) {
	t.Helper()
	objPath = "/tmp/gaston-test-libgastonc-stdio.o"
	libPath = "/tmp/gaston-test-libgastonc.a"
	t.Cleanup(func() { os.Remove(objPath); os.Remove(libPath) })

	if err := compileObjPath("libc/stdio.cm", objPath, nil); err != nil {
		t.Fatalf("compile stdio.cm: %v", err)
	}
	if err := archiveCreate(libPath, []string{objPath}); err != nil {
		t.Fatalf("archive libgastonc.a: %v", err)
	}
	return libPath, objPath
}

// TestLibc compiles programs against libgastonc.a (the gaston standard C library)
// and runs them in an Alpine ARM64 container.
func TestLibc(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker not found in PATH; skipping container tests")
	}

	libPath, _ := buildLibgastonc(t)
	includePaths := []string{"libc"}

	for _, tt := range libcTests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			// Compile the main program with the libc include path.
			mainSrc := fmt.Sprintf("testdata/%s.cm", tt.name)
			mainObj := fmt.Sprintf("/tmp/gaston-test-%s.o", tt.name)
			t.Cleanup(func() { os.Remove(mainObj) })
			if err := compileObjPath(mainSrc, mainObj, includePaths); err != nil {
				t.Fatalf("compile %s: %v", tt.name, err)
			}

			// Link: main.o + libgastonc.a → binary (lazy linking).
			binPath := fmt.Sprintf("/tmp/gaston-test-%s", tt.name)
			t.Cleanup(func() { os.Remove(binPath) })
			if err := link(binPath, []string{mainObj, libPath}); err != nil {
				t.Fatalf("link: %v", err)
			}

			// Run in Alpine ARM64 container.
			cmd := exec.Command("docker", "run", "--rm",
				"--platform", "linux/arm64",
				"-i",
				"-v", binPath+":/prog",
				"alpine:latest",
				"/prog",
			)
			out, err := cmd.Output()
			if err != nil {
				if ee, ok := err.(*exec.ExitError); ok {
					t.Fatalf("docker run failed (exit %d):\nstderr: %s",
						ee.ExitCode(), string(ee.Stderr))
				}
				t.Fatalf("docker run: %v", err)
			}

			got := string(out)
			if got != tt.want {
				t.Errorf("output mismatch:\n  got  %q\n  want %q", got, tt.want)
			}
		})
	}
}

// semErrorTest describes a program that must fail semCheck with a specific error.
type semErrorTest struct {
	name string // testdata/<name>.cm
	want string // expected substring in the semCheck error
}

var semErrorTests = []semErrorTest{
	// ── Item 7: pointer assignment type checking ──────────────────────────
	// Assigning int* to char* is incompatible (neither is void*).
	{name: "err_ptr_incompat", want: "assignment of incompatible pointer types"},
	// Assigning a non-zero integer to a pointer is not allowed.
	{name: "err_ptr_int", want: "assignment of non-pointer to pointer type"},
	// Assigning double* to int* is incompatible (FP pointer types are not interchangeable).
	{name: "err_ptr_fp", want: "assignment of incompatible pointer types"},
	// ── Item 9: const pointer target ─────────────────────────────────────
	// Assigning through a const-qualified pointer must be rejected.
	{name: "err_const_ptr", want: "assignment to const-qualified pointer target"},
}

// TestSemErrors verifies that ill-typed programs are rejected by semCheck with
// the expected error message.
func TestSemErrors(t *testing.T) {
	for _, tt := range semErrorTests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			srcPath := fmt.Sprintf("testdata/%s.cm", tt.name)
			raw, err := os.ReadFile(srcPath)
			if err != nil {
				t.Fatalf("read %s: %v", srcPath, err)
			}
			pp := newPreprocessor(nil)
			src, err2 := pp.Preprocess(string(raw), srcPath)
			if err2 != nil {
				t.Fatalf("preprocess: %v", err2)
			}
			lex := newLexer(src, srcPath)
			yyParse(lex)
			if lex.errors > 0 {
				t.Fatalf("parse errors in %s", tt.name)
			}
			semErr := semCheck(lex.result, false)
			if semErr == nil {
				t.Fatalf("%s: expected semCheck error containing %q, got none", tt.name, tt.want)
			}
			if !strings.Contains(semErr.Error(), tt.want) {
				t.Errorf("%s: error %q does not contain %q", tt.name, semErr.Error(), tt.want)
			}
		})
	}
}

// TestDockerRun compiles each test program and runs it in an Alpine ARM64
// container, comparing stdout to the expected string.
func TestDockerRun(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker not found in PATH; skipping container tests")
	}

	for _, tt := range featureTests {
		tt := tt // capture loop variable
		t.Run(tt.name, func(t *testing.T) {
			binPath := fmt.Sprintf("/tmp/gaston-test-%s", tt.name)
			t.Cleanup(func() { os.Remove(binPath) })

			if err := compileTest(tt.name, binPath); err != nil {
				t.Fatalf("compile: %v", err)
			}

			cmd := exec.Command("docker", "run", "--rm",
				"--platform", "linux/arm64",
				"-i",
				"-v", binPath+":/prog",
				"alpine:latest",
				"/prog",
			)
			cmd.Stdin = strings.NewReader(tt.stdin)
			out, err := cmd.Output()
			if err != nil {
				if ee, ok := err.(*exec.ExitError); ok {
					t.Fatalf("docker run failed (exit %d):\nstderr: %s",
						ee.ExitCode(), string(ee.Stderr))
				}
				t.Fatalf("docker run: %v", err)
			}

			got := string(out)
			if got != tt.want {
				t.Errorf("output mismatch:\n  got  %q\n  want %q", got, tt.want)
			}
		})
	}
}
