// Command gaston is a C compiler targeting Linux ARM64.
//
// Default mode compiles .c/.cm sources to in-memory objects, links them with
// any .o/.a files and -l libraries, and writes a ready-to-run ELF executable.
//
// Usage:
//
//	gaston [-I dir] [-L dir] [-l lib] [-o out] file.c [file2.c ...] [lib.a]
//	gaston -c [-o out.o] <file.c>       — compile to relocatable object
//	gaston -link -o <out> a.o b.o …     — link object files to ELF binary
//	gaston -asm <file.cm>               — emit Plan 9 .s + Go bridge (legacy)
//
//go:generate go tool golang.org/x/tools/cmd/goyacc -o parser.go grammar.y
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// defaultIncludes, defaultLibPaths, and defaultLibs are injected at build time
// via -ldflags "-X main.defaultIncludes=... -X main.defaultLibPaths=... -X main.defaultLibs=..."
// for target installs (e.g. mazarin). They are empty for dev/test builds.
// Multiple entries are separated by colons (paths) or commas (lib names).
var (
	defaultIncludes = "" // e.g. "/gaston/include"
	defaultLibPaths = "" // e.g. "/gaston/lib"
	defaultLibs     = "" // e.g. "gastonc"
)

// isIgnoredFlag reports whether a flag name should be silently ignored.
// This allows gaston to be used as a drop-in compiler in build systems
// (e.g. meson) that pass GCC/Clang-specific flags.
func isIgnoredFlag(name string) bool {
	// Strip leading dashes to get the bare name.
	bare := strings.TrimLeft(name, "-")
	// Ignore all warning flags and error-on-warning flags.
	if strings.HasPrefix(bare, "W") || strings.HasPrefix(bare, "w") {
		return true
	}
	// Ignore all machine-tuning flags.
	if strings.HasPrefix(bare, "march=") || strings.HasPrefix(bare, "mcpu=") ||
		strings.HasPrefix(bare, "mtune=") || strings.HasPrefix(bare, "mabi=") ||
		strings.HasPrefix(bare, "mfloat-abi=") || strings.HasPrefix(bare, "mfpu=") ||
		strings.HasPrefix(bare, "msave-restore") || strings.HasPrefix(bare, "mcmodel=") ||
		strings.HasPrefix(bare, "mno-") || strings.HasPrefix(bare, "mthumb") ||
		strings.HasPrefix(bare, "marm") {
		return true
	}
	// Ignore code-generation and optimization flags that don't affect gaston's output.
	ignored := map[string]bool{
		"ffreestanding": true, "fno-common": true, "ffunction-sections": true,
		"fdata-sections": true, "fno-stack-protector": true, "fno-builtin": true,
		"fno-plt": true, "fomit-frame-pointer": true, "fno-strict-aliasing": true,
		"fno-exceptions": true, "fno-unwind-tables": true, "fno-asynchronous-unwind-tables": true,
		"frounding-math": true, "fsignaling-nans": true, "fvisibility=hidden": true,
		"fno-pic": true, "fpic": true, "fPIC": true, "fpie": true, "fPIE": true,
		"nostdlib": true, "nodefaultlibs": true, "nostartfiles": true,
		"pipe": true, "g": true, "g0": true, "g1": true, "g2": true, "g3": true,
		"Os": true, "O0": true, "O1": true, "O2": true, "O3": true,
		// Dependency-file generation flags (make-compatible .d files).
		"MD": true, "MMD": true, "MP": true,
		// Diagnostic formatting flags.
		"fdiagnostics-color": true,
		// PCH flags.
		"Winvalid-pch": true,
		// Extra warning sets (all -W flags are already caught above, but be explicit).
		"Wextra": true,
	}
	if ignored[bare] {
		return true
	}
	// Ignore -fdiagnostics-color=<value>, -MQ, -MF (take a value token).
	if strings.HasPrefix(bare, "fdiagnostics-color=") || strings.HasPrefix(bare, "fdiagnostics-") {
		return true
	}
	// Ignore -fno-builtin-<name>, -fstrict-flex-arrays=N, -fstack-usage, etc.
	if strings.HasPrefix(bare, "fno-builtin-") || strings.HasPrefix(bare, "fstrict-") ||
		strings.HasPrefix(bare, "fstack-") || strings.HasPrefix(bare, "fsanitize") ||
		strings.HasPrefix(bare, "fcoverage") || strings.HasPrefix(bare, "fprofile") ||
		strings.HasPrefix(bare, "flto") || strings.HasPrefix(bare, "fuse-ld=") {
		return true
	}
	// Ignore C/C++ standard selection and language flags.
	if strings.HasPrefix(bare, "std=") || bare == "x" {
		return true
	}
	// Ignore debug/profiling info flags.
	if bare == "g" || strings.HasPrefix(bare, "g=") {
		return true
	}
	// Ignore preprocessor output-format flags (used by build systems to query macros).
	if bare == "dM" || bare == "dD" || bare == "dN" || bare == "dI" || bare == "dU" {
		return true
	}
	// Ignore GCC preprocessor formatting flags that don't affect output content.
	if bare == "P" || bare == "C" || bare == "CC" || bare == "H" || bare == "undef" {
		return true
	}
	// Ignore verbose flag (GCC -v prints include paths; meson uses it for discovery
	// but gracefully handles non-GCC output with a WARNING and continues).
	if bare == "v" {
		return true
	}
	return false
}

// gccPredefinedMacros returns the minimal set of GCC predefined macros for
// ARM64 Linux that build systems expect from `cc -E -dM -`.
// These mirror what GCC 13 emits for aarch64-linux-gnu.
func gccPredefinedMacros() string {
	return `#define __aarch64__ 1
#define __ARM_ARCH 8
#define __ARM_ARCH_8A__ 1
#define __arm__ 0
#define __linux__ 1
#define __linux 1
#define linux 1
#define __unix__ 1
#define __unix 1
#define __BYTE_ORDER__ __ORDER_LITTLE_ENDIAN__
#define __ORDER_LITTLE_ENDIAN__ 1234
#define __ORDER_BIG_ENDIAN__ 4321
#define __LITTLE_ENDIAN__ 1
#define __LP64__ 1
#define _LP64 1
#define __SIZEOF_POINTER__ 8
#define __SIZEOF_INT__ 4
#define __SIZEOF_LONG__ 8
#define __SIZEOF_LONG_LONG__ 8
#define __SIZEOF_SHORT__ 2
#define __SIZEOF_FLOAT__ 4
#define __SIZEOF_DOUBLE__ 8
#define __SIZEOF_LONG_DOUBLE__ 16
#define __SIZEOF_SIZE_T__ 8
#define __INT8_TYPE__ signed char
#define __INT16_TYPE__ short
#define __INT32_TYPE__ int
#define __INT64_TYPE__ long int
#define __UINT8_TYPE__ unsigned char
#define __UINT16_TYPE__ unsigned short
#define __UINT32_TYPE__ unsigned int
#define __UINT64_TYPE__ long unsigned int
#define __INTPTR_TYPE__ long int
#define __UINTPTR_TYPE__ long unsigned int
#define __SIZE_TYPE__ long unsigned int
#define __PTRDIFF_TYPE__ long int
#define __CHAR_BIT__ 8
#define __GNUC__ 13
#define __GNUC_MINOR__ 1
#define __GNUC_PATCHLEVEL__ 0
#define __GNUC_STDC_INLINE__ 1
#define __STDC__ 1
#define __STDC_VERSION__ 201710L
#define __STDC_HOSTED__ 0
#define __VERSION__ "13.1.0"
#define __GCC_HAVE_SYNC_COMPARE_AND_SWAP_1 1
#define __GCC_HAVE_SYNC_COMPARE_AND_SWAP_2 1
#define __GCC_HAVE_SYNC_COMPARE_AND_SWAP_4 1
#define __GCC_HAVE_SYNC_COMPARE_AND_SWAP_8 1
#define __USER_LABEL_PREFIX__
`
}

// filterArgs strips flags that gaston intentionally ignores (e.g. GCC/Clang
// machine or warning flags passed by build systems like meson) from args,
// returning the filtered list. It also detects --version / -v requests and
// whether -dM (dump predefined macros) was present.
func filterArgs(args []string) (filtered []string, versionRequested bool, dumpMacros bool) {
	// ignoredValueFlags are flags that should be silently consumed along with
	// their value token (both the flag and its argument are dropped).
	ignoredValueFlags := map[string]bool{
		"MQ": true, "MF": true, "MT": true, // dependency file target/file (ignored)
		"include": true,                     // force-include file (gaston doesn't support; meson uses picolibc.h)
		"isystem": true,                     // add system include path (treat as -I via valueFlags below)
	}

	// valueFlags are flags that consume the next token as their value when
	// written in two-token form (e.g. -o outfile, -I includedir).
	valueFlags := map[string]bool{"o": true, "I": true, "D": true, "L": true, "l": true, "U": true}

	var positional []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--version" {
			versionRequested = true
			continue
		}
		if arg == "-dM" || arg == "--dM" {
			dumpMacros = true
			continue
		}
		// Non-flag argument (positional) or bare "-" (stdin).
		if !strings.HasPrefix(arg, "-") || arg == "-" {
			positional = append(positional, arg)
			continue
		}
		if isIgnoredFlag(arg) {
			continue
		}
		// Handle long-form ignored value flags: -MQ <val>, -MF <val>, -include <file>, etc.
		// These consume the next token as their value and both are dropped.
		{
			bare2 := strings.TrimLeft(arg, "-")
			if ignoredValueFlags[bare2] {
				if i+1 < len(args) {
					i++ // consume the value token too
				}
				continue
			}
		}
		// Split GCC-style combined flag+value (-DFOO, -Idir, -lfoo, -ofoo, -UMACRO).
		// Go's flag package only understands -flag=val or -flag val (two tokens).
		if len(arg) > 2 && arg[0] == '-' && arg[1] != '-' {
			name := string(arg[1])
			if valueFlags[name] {
				if name == "U" {
					// -UMACRO: pass as -D MACRO= (undefine is not natively supported;
					// just ignore since gaston's preprocessor doesn't predefine much).
					continue
				}
				filtered = append(filtered, "-"+name, arg[2:])
				continue
			}
		}
		// Two-token form: -o file, -I dir, -D MACRO, -U MACRO, etc.
		// Consume the value token now so it stays paired with its flag and
		// doesn't end up in positional[].
		if len(arg) >= 2 && arg[0] == '-' && arg[1] != '-' {
			name := string(arg[1:])
			if ignoredValueFlags[name] {
				if i+1 < len(args) {
					i++ // consume value
				}
				continue
			}
			if len(name) == 1 && valueFlags[name] {
				if name == "U" {
					if i+1 < len(args) {
						i++ // consume the macro name, drop both
					}
					continue
				}
				if i+1 < len(args) {
					filtered = append(filtered, arg, args[i+1])
					i++
				} else {
					filtered = append(filtered, arg)
				}
				continue
			}
		}
		filtered = append(filtered, arg)
	}
	// Put flags before positional args so flag.Parse() sees all flags even
	// when build systems (meson, make) put source files before flags.
	filtered = append(filtered, positional...)
	return
}

func main() {
	// Pre-filter os.Args to strip unknown flags before flag.Parse sees them.
	filtered, versionRequested, dumpMacros := filterArgs(os.Args[1:])

	// Handle GCC-compatible introspection flags before flag.Parse.
	// Build systems (meson, cmake, autoconf) probe compilers with these.
	for _, arg := range os.Args[1:] {
		switch arg {
		case "-dumpversion", "--dumpversion":
			fmt.Println("13.1.0")
			os.Exit(0)
		case "-dumpmachine", "--dumpmachine":
			fmt.Println("aarch64-linux-gnu")
			os.Exit(0)
		case "-dumpfullversion", "--dumpfullversion":
			fmt.Println("13.1.0")
			os.Exit(0)
		case "-print-search-dirs":
			os.Exit(0)
		case "-print-libgcc-file-name":
			fmt.Println("")
			os.Exit(0)
		case "-Wl,--version":
			// Meson probes the linker type via `cc -Wl,--version`.
			// Respond with GNU ld-compatible output so meson selects the "ld" linker.
			fmt.Fprintln(os.Stderr, "GNU ld (GNU Binutils) 2.38")
			os.Exit(0)
		}
	}

	if versionRequested {
		// GCC-compatible version string so that build systems (meson, cmake, etc.)
		// correctly identify gaston as GCC-compatible.
		fmt.Println("gaston (GCC) 13.1.0")
		fmt.Println("Copyright (C) 2023 Free Software Foundation, Inc.")
		fmt.Println("This is free software; see the source for copying conditions.")
		os.Exit(0)
	}
	// Replace os.Args with the filtered list so flag.Parse works normally.
	os.Args = append(os.Args[:1], filtered...)

	asmMode    := flag.Bool("asm", false, "emit Plan 9 ARM64 assembly + Go bridge instead of ELF")
	compOnly   := flag.Bool("c", false, "compile to relocatable object (.o) only; do not link")
	linkMode   := flag.Bool("link", false, "link mode: combine .o/.a files into an ELF executable")
	arMode     := flag.Bool("ar", false, "archive mode: package .o files into a static library (.a)")
	preprocOnly := flag.Bool("preprocess", false, "stop after preprocessing; write <base>.pre.c")
	preprocStdout := flag.Bool("E", false, "preprocess to stdout (GCC-compatible)")
	outFlag    := flag.String("o", "", "output file name (used with -c, -link, or -ar)")
	var includePaths includeFlags
	var defines defineFlags
	var libPaths libPathFlags
	var libs libFlags
	flag.Var(&includePaths, "I", "add `directory` to the include search path (may be repeated)")
	flag.Var(&defines, "D", "define preprocessor macro `NAME[=value]` (may be repeated)")
	flag.Var(&libPaths, "L", "add `directory` to the library search path (may be repeated)")
	flag.Var(&libs, "l", "link against lib`name` (searches -L paths for libname.a)")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage:\n")
		fmt.Fprintf(os.Stderr, "  gaston [flags] file.c [file2.c ...] [lib.a]  — compile + link to ELF\n")
		fmt.Fprintf(os.Stderr, "  gaston -c [-o out.o] <file.c>                — compile to object file\n")
		fmt.Fprintf(os.Stderr, "  gaston -link -o out a.o b.o …                — link objects/archives\n")
		fmt.Fprintf(os.Stderr, "  gaston -ar -o libfoo.a a.o b.o …             — build static library\n")
		fmt.Fprintf(os.Stderr, "  gaston -preprocess <file.c>                  — preprocess only\n")
		fmt.Fprintf(os.Stderr, "\nflags:\n")
		fmt.Fprintf(os.Stderr, "  -I <dir>          add include search path\n")
		fmt.Fprintf(os.Stderr, "  -D NAME[=val]     define preprocessor macro\n")
		fmt.Fprintf(os.Stderr, "  -L <dir>          add library search path\n")
		fmt.Fprintf(os.Stderr, "  -l <name>         link against lib<name>.a\n")
		fmt.Fprintf(os.Stderr, "  -o <file>         output file name\n")
	}
	flag.Parse()

	// Append built-in defaults (set at build time via -ldflags) after any
	// explicit flags, so explicit -I/-L/-l always take precedence.
	if defaultIncludes != "" {
		for _, p := range strings.Split(defaultIncludes, ":") {
			if p != "" {
				includePaths = append(includePaths, p)
			}
		}
	}
	if defaultLibPaths != "" {
		for _, p := range strings.Split(defaultLibPaths, ":") {
			if p != "" {
				libPaths = append(libPaths, p)
			}
		}
	}
	if defaultLibs != "" {
		for _, l := range strings.Split(defaultLibs, ",") {
			if l != "" {
				libs = append(libs, l)
			}
		}
	}

	// ── archive mode ──────────────────────────────────────────────────────
	if *arMode {
		if flag.NArg() == 0 {
			fmt.Fprintf(os.Stderr, "gaston: -ar requires one or more .o files\n")
			os.Exit(1)
		}
		outFile := *outFlag
		if outFile == "" {
			fmt.Fprintf(os.Stderr, "gaston: -ar requires -o <output.a>\n")
			os.Exit(1)
		}
		if err := archiveCreate(outFile, flag.Args()); err != nil {
			fmt.Fprintf(os.Stderr, "gaston: %v\n", err)
			os.Exit(1)
		}
		fmt.Fprintf(os.Stderr, "gaston: archive %s\n", outFile)
		return
	}

	// ── linker mode ───────────────────────────────────────────────────────
	if *linkMode {
		if flag.NArg() == 0 {
			fmt.Fprintf(os.Stderr, "gaston: -link requires one or more .o/.a files\n")
			os.Exit(1)
		}
		outFile := *outFlag
		if outFile == "" {
			outFile = "a.elf"
		}
		// Resolve -l flags into archive paths and append to input list.
		// Go's flag package stops parsing at the first non-flag arg, so
		// -L / -l flags that appear after positional args (e.g. "lua.o liblua.a -L /lib -l foo")
		// end up in flag.Args() rather than being handled by flag.Var.
		// Extract them manually from the remaining args.
		rawArgs := flag.Args()
		var inputs []string
		for i := 0; i < len(rawArgs); i++ {
			arg := rawArgs[i]
			if arg == "-L" && i+1 < len(rawArgs) {
				libPaths = append(libPaths, rawArgs[i+1])
				i++
			} else if strings.HasPrefix(arg, "-L") && len(arg) > 2 {
				libPaths = append(libPaths, arg[2:])
			} else if arg == "-l" && i+1 < len(rawArgs) {
				libs = append(libs, rawArgs[i+1])
				i++
			} else if strings.HasPrefix(arg, "-l") && len(arg) > 2 {
				libs = append(libs, arg[2:])
			} else {
				inputs = append(inputs, arg)
			}
		}
		for _, lib := range libs {
			path, err := resolveLib(lib, []string(libPaths))
			if err != nil {
				fmt.Fprintf(os.Stderr, "gaston: %v\n", err)
				os.Exit(1)
			}
			inputs = append(inputs, path)
		}
		if err := link(outFile, inputs); err != nil {
			fmt.Fprintf(os.Stderr, "gaston: %v\n", err)
			os.Exit(1)
		}
		fmt.Fprintf(os.Stderr, "gaston: linked %s\n", outFile)
		return
	}

	// ── -E: preprocess to stdout (GCC-compatible, used by build systems) ──
	if *preprocStdout {
		// -E -dM: dump predefined macros (meson uses this to identify the compiler).
		if dumpMacros {
			fmt.Print(gccPredefinedMacros())
			return
		}
		var rawSrc []byte
		var filename string
		if flag.NArg() == 0 || flag.Arg(0) == "-" {
			rawSrc, _ = io.ReadAll(os.Stdin)
			filename = "<stdin>"
		} else {
			filename = flag.Arg(0)
			var err error
			rawSrc, err = os.ReadFile(filename)
			if err != nil {
				fmt.Fprintf(os.Stderr, "gaston: %v\n", err)
				os.Exit(1)
			}
		}
		pp := newPreprocessor([]string(includePaths), []string(defines))
		src, err := pp.Preprocess(string(rawSrc), filename)
		if err != nil {
			fmt.Fprintf(os.Stderr, "gaston: %v\n", err)
			os.Exit(1)
		}
		fmt.Print(src)
		return
	}

	// ── single-file modes: -c, -preprocess, -asm ─────────────────────────
	if *compOnly || *preprocOnly || *asmMode {
		if flag.NArg() != 1 {
			flag.Usage()
			os.Exit(1)
		}
		infile := flag.Arg(0)
		ext := filepath.Ext(infile)
		base := strings.TrimSuffix(filepath.Base(infile), ext)
		dir := filepath.Dir(infile)

		// Assembly files (.S / .s) are preprocessed by the C preprocessor and
		// then assembled. Delegate to gcc since gaston has no assembler backend.
		if *compOnly && (ext == ".S" || ext == ".s") {
			outFile := *outFlag
			if outFile == "" {
				outFile = filepath.Join(dir, base+".o")
			}
			gccArgs := []string{"-c", "-o", outFile}
			for _, p := range includePaths {
				gccArgs = append(gccArgs, "-I", p)
			}
			for _, d := range defines {
				gccArgs = append(gccArgs, "-D", d)
			}
			gccArgs = append(gccArgs, infile)
			cmd := exec.Command("gcc", gccArgs...)
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			if err := cmd.Run(); err != nil {
				fmt.Fprintf(os.Stderr, "gaston: assembler failed: %v\n", err)
				os.Exit(1)
			}
			fmt.Fprintf(os.Stderr, "gaston: wrote %s\n", outFile)
			return
		}

		rawSrc, err := os.ReadFile(infile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "gaston: %v\n", err)
			os.Exit(1)
		}
		pp := newPreprocessor([]string(includePaths), []string(defines))
		src, err := pp.Preprocess(string(rawSrc), infile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "gaston: %v\n", err)
			os.Exit(1)
		}

		if *preprocOnly {
			outFile := filepath.Join(dir, base+".pre.c")
			if err := os.WriteFile(outFile, []byte(src), 0644); err != nil {
				fmt.Fprintf(os.Stderr, "gaston: %v\n", err)
				os.Exit(1)
			}
			fmt.Fprintf(os.Stderr, "gaston: wrote %s\n", outFile)
			return
		}

		lex := newLexer(src, infile)
		yyParse(lex)
		if lex.errors > 0 {
			fmt.Fprintf(os.Stderr, "gaston: %d error(s), aborting\n", lex.errors)
			os.Exit(1)
		}
		if lex.result == nil {
			fmt.Fprintf(os.Stderr, "gaston: empty program\n")
			os.Exit(1)
		}
		if err := semCheck(lex.result, false); err != nil {
			fmt.Fprintf(os.Stderr, "gaston: %v\n", err)
			os.Exit(1)
		}
		irp := genIR(lex.result)

		if *asmMode {
			asmFile := filepath.Join(dir, base+".s")
			asmOut, err := os.Create(asmFile)
			if err != nil {
				fmt.Fprintf(os.Stderr, "gaston: %v\n", err)
				os.Exit(1)
			}
			genARM64(irp, asmOut)
			asmOut.Close()
			fmt.Fprintf(os.Stderr, "gaston: wrote %s\n", asmFile)
			if err := genRuntime(filepath.Join(dir, base)); err != nil {
				fmt.Fprintf(os.Stderr, "gaston: %v\n", err)
				os.Exit(1)
			}
			return
		}

		// -c: compile to .o file on disk.
		outFile := *outFlag
		if outFile == "" {
			outFile = filepath.Join(dir, base+".o")
		}
		if err := genObjectFile(irp, outFile); err != nil {
			fmt.Fprintf(os.Stderr, "gaston: %v\n", err)
			os.Exit(1)
		}
		fmt.Fprintf(os.Stderr, "gaston: wrote %s\n", outFile)
		return
	}

	// ── default mode: compile + link ──────────────────────────────────────
	// Accepts a mix of .c/.cm source files, .o object files, and .a archives.
	// Source files are compiled to in-memory objects; everything is linked
	// into a single ELF executable.
	if flag.NArg() == 0 {
		flag.Usage()
		os.Exit(1)
	}

	var objs []*objFile
	var archivePaths []string
	firstName := ""

	for _, arg := range flag.Args() {
		ext := filepath.Ext(arg)
		switch ext {
		case ".c":
			if firstName == "" {
				firstName = strings.TrimSuffix(filepath.Base(arg), ext)
			}
			rawSrc, err := os.ReadFile(arg)
			if err != nil {
				fmt.Fprintf(os.Stderr, "gaston: %v\n", err)
				os.Exit(1)
			}
			pp := newPreprocessor([]string(includePaths), []string(defines))
			src, err := pp.Preprocess(string(rawSrc), arg)
			if err != nil {
				fmt.Fprintf(os.Stderr, "gaston: %v\n", err)
				os.Exit(1)
			}
			lex := newLexer(src, arg)
			yyParse(lex)
			if lex.errors > 0 {
				fmt.Fprintf(os.Stderr, "gaston: %d error(s) in %s\n", lex.errors, arg)
				os.Exit(1)
			}
			if lex.result == nil {
				fmt.Fprintf(os.Stderr, "gaston: empty program: %s\n", arg)
				os.Exit(1)
			}
			if err := semCheck(lex.result, false); err != nil {
				fmt.Fprintf(os.Stderr, "gaston: %v\n", err)
				os.Exit(1)
			}
			irp := genIR(lex.result)
			objData, err := genObjectBytes(irp)
			if err != nil {
				fmt.Fprintf(os.Stderr, "gaston: %v\n", err)
				os.Exit(1)
			}
			obj, err := loadObjFromBytes(arg, objData)
			if err != nil {
				fmt.Fprintf(os.Stderr, "gaston: %v\n", err)
				os.Exit(1)
			}
			objs = append(objs, obj)
		case ".S", ".s":
			// Delegate assembly to gcc; write a temp .o and load it.
			tmpObj, err := os.CreateTemp("", "gaston-asm-*.o")
			if err != nil {
				fmt.Fprintf(os.Stderr, "gaston: %v\n", err)
				os.Exit(1)
			}
			tmpObj.Close()
			gccArgs := []string{"-c", "-o", tmpObj.Name()}
			for _, p := range includePaths {
				gccArgs = append(gccArgs, "-I", p)
			}
			for _, d := range defines {
				gccArgs = append(gccArgs, "-D", d)
			}
			gccArgs = append(gccArgs, arg)
			cmd := exec.Command("gcc", gccArgs...)
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			if err := cmd.Run(); err != nil {
				fmt.Fprintf(os.Stderr, "gaston: assembler failed for %s: %v\n", arg, err)
				os.Exit(1)
			}
			obj, err := loadObjFile(tmpObj.Name())
			if err != nil {
				fmt.Fprintf(os.Stderr, "gaston: %v\n", err)
				os.Exit(1)
			}
			objs = append(objs, obj)
		case ".o":
			obj, err := loadObjFile(arg)
			if err != nil {
				fmt.Fprintf(os.Stderr, "gaston: %v\n", err)
				os.Exit(1)
			}
			objs = append(objs, obj)
		case ".a":
			archivePaths = append(archivePaths, arg)
		default:
			fmt.Fprintf(os.Stderr, "gaston: unrecognized file type: %s\n", arg)
			os.Exit(1)
		}
	}

	// Resolve -l flags into archive paths.
	for _, lib := range libs {
		path, err := resolveLib(lib, []string(libPaths))
		if err != nil {
			fmt.Fprintf(os.Stderr, "gaston: %v\n", err)
			os.Exit(1)
		}
		archivePaths = append(archivePaths, path)
	}

	outFile := *outFlag
	if outFile == "" {
		outFile = firstName
	}
	if outFile == "" {
		outFile = "a.out"
	}

	if err := linkWithObjs(outFile, objs, archivePaths); err != nil {
		fmt.Fprintf(os.Stderr, "gaston: %v\n", err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "gaston: wrote %s\n", outFile)
}
