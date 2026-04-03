// Command gaston is a C-minus compiler targeting Linux ARM64.
//
// Default mode: reads <file.cm>, writes a ready-to-run Linux ARM64 ELF binary.
//
// Usage:
//
//	gaston <file.cm>           — compile to ELF binary <file>
//	gaston -asm <file.cm>      — compile to Plan 9 .s + Go bridge _rt.go (legacy)
//
//go:generate goyacc -o parser.go grammar.y
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func main() {
	asmMode := flag.Bool("asm", false, "emit Plan 9 ARM64 assembly + Go bridge instead of ELF")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: gaston [-asm] <file.cm>\n")
		fmt.Fprintf(os.Stderr, "Compiles C-minus source to a Linux ARM64 ELF binary (default)\n")
		fmt.Fprintf(os.Stderr, "or Plan 9 ARM64 assembly + Go bridge (-asm).\n")
	}
	flag.Parse()

	if flag.NArg() != 1 {
		flag.Usage()
		os.Exit(1)
	}

	infile := flag.Arg(0)
	ext := filepath.Ext(infile)
	base := strings.TrimSuffix(filepath.Base(infile), ext)
	dir := filepath.Dir(infile)

	// Read source file.
	src, err := os.ReadFile(infile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "gaston: %v\n", err)
		os.Exit(1)
	}

	// Parse.
	lex := newLexer(string(src), infile)
	yyParse(lex)
	if lex.errors > 0 {
		fmt.Fprintf(os.Stderr, "gaston: %d error(s), aborting\n", lex.errors)
		os.Exit(1)
	}
	if lex.result == nil {
		fmt.Fprintf(os.Stderr, "gaston: empty program\n")
		os.Exit(1)
	}

	// Semantic analysis.
	if err := semCheck(lex.result); err != nil {
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

	// Default: emit Linux ARM64 ELF binary.
	outFile := filepath.Join(dir, base)
	if err := genELF(irp, outFile); err != nil {
		fmt.Fprintf(os.Stderr, "gaston: %v\n", err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "gaston: wrote %s\n", outFile)
}
