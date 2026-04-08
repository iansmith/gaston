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
	"os"
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

func main() {
	asmMode    := flag.Bool("asm", false, "emit Plan 9 ARM64 assembly + Go bridge instead of ELF")
	compOnly   := flag.Bool("c", false, "compile to relocatable object (.o) only; do not link")
	linkMode   := flag.Bool("link", false, "link mode: combine .o/.a files into an ELF executable")
	arMode     := flag.Bool("ar", false, "archive mode: package .o files into a static library (.a)")
	preprocOnly := flag.Bool("preprocess", false, "stop after preprocessing; write <base>.pre.c")
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
		inputs := flag.Args()
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
