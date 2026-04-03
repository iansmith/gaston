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

	// ── Integration ──────────────────────────────────────────────────────
	{name: "combo_all", want: "63\nABC\nok\n"},
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
	src, err := os.ReadFile(srcPath)
	if err != nil {
		return fmt.Errorf("read %s: %w", srcPath, err)
	}
	lex := newLexer(string(src), srcPath)
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
	src, err := os.ReadFile(srcPath)
	if err != nil {
		return fmt.Errorf("read %s: %w", srcPath, err)
	}
	lex := newLexer(string(src), srcPath)
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
