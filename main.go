// Command gaston is a C-minus compiler targeting Linux ARM64.
//
// Default mode: reads <file.cm>, writes a ready-to-run Linux ARM64 ELF binary.
//
// Usage:
//
//	gaston <file.cm>                    — compile to ELF binary <file>
//	gaston -c <file.cm>                 — compile to relocatable object <file.o>
//	gaston -link -o <out> a.o b.o …    — link object files to ELF binary
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

func main() {
	asmMode    := flag.Bool("asm", false, "emit Plan 9 ARM64 assembly + Go bridge instead of ELF")
	compOnly   := flag.Bool("c", false, "compile to relocatable object (.o) only; do not link")
	linkMode   := flag.Bool("link", false, "link mode: combine .o/.a files into an ELF executable")
	arMode     := flag.Bool("ar", false, "archive mode: package .o files into a static library (.a)")
	preprocOnly := flag.Bool("preprocess", false, "stop after preprocessing; write <base>.pre.cm")
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
		fmt.Fprintf(os.Stderr, "  gaston <file.cm>                    — compile to ELF binary\n")
		fmt.Fprintf(os.Stderr, "  gaston -c [-o out.o] <file.cm>      — compile to object file\n")
		fmt.Fprintf(os.Stderr, "  gaston -link -o out a.o b.o …       — link objects/archives\n")
		fmt.Fprintf(os.Stderr, "  gaston -ar -o libfoo.a a.o b.o …    — build static library\n")
		fmt.Fprintf(os.Stderr, "  gaston -asm <file.cm>               — emit Plan 9 assembly (legacy)\n")
		fmt.Fprintf(os.Stderr, "  gaston -preprocess <file.cm>        — preprocess only; write <base>.pre.cm\n")
		fmt.Fprintf(os.Stderr, "  gaston -I <dir> <file.cm>           — add include search path\n")
		fmt.Fprintf(os.Stderr, "  gaston -D NAME[=val] <file.cm>      — define preprocessor macro\n")
		fmt.Fprintf(os.Stderr, "  gaston -L <dir> -l <name>           — library search path / library\n")
	}
	flag.Parse()

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

	// ── compiler mode ─────────────────────────────────────────────────────
	if flag.NArg() != 1 {
		flag.Usage()
		os.Exit(1)
	}

	infile := flag.Arg(0)
	ext := filepath.Ext(infile)
	base := strings.TrimSuffix(filepath.Base(infile), ext)
	dir := filepath.Dir(infile)

	// Read source file.
	rawSrc, err := os.ReadFile(infile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "gaston: %v\n", err)
		os.Exit(1)
	}

	// Preprocess.
	pp := newPreprocessor([]string(includePaths), []string(defines))
	src, err := pp.Preprocess(string(rawSrc), infile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "gaston: %v\n", err)
		os.Exit(1)
	}

	if *preprocOnly {
		outFile := filepath.Join(dir, base+".pre.cm")
		if err := os.WriteFile(outFile, []byte(src), 0644); err != nil {
			fmt.Fprintf(os.Stderr, "gaston: %v\n", err)
			os.Exit(1)
		}
		fmt.Fprintf(os.Stderr, "gaston: wrote %s\n", outFile)
		return
	}

	// Parse.
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

	// Semantic analysis. In -c mode, main() is not required.
	if err := semCheck(lex.result, !*compOnly); err != nil {
		fmt.Fprintf(os.Stderr, "gaston: %v\n", err)
		os.Exit(1)
	}

	// IR generation.
	irp := genIR(lex.result)

	if *asmMode {
		// Legacy: emit Plan 9 assembly + Go runtime bridge.
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

	if *compOnly {
		// Compile-only: emit ET_REL object file.
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

	// Default: emit Linux ARM64 ELF binary.
	outFile := *outFlag
	if outFile == "" {
		outFile = filepath.Join(dir, base+".elf")
	}
	if err := genELF(irp, outFile); err != nil {
		fmt.Fprintf(os.Stderr, "gaston: %v\n", err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "gaston: wrote %s\n", outFile)
}
