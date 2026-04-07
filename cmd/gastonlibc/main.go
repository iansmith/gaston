// gastonlibc compiles picolibc and gaston libc sources into libgastonc.a.
//
// Usage:
//
//	go tool gastonlibc -go <path> -o build/libgastonc.a
//	go tool gastonlibc -go <path> -test   # just compile, don't archive
//
// The tool runs from the cmd/gaston directory (same as the tests).
package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// sourceGroup describes a directory of C sources to compile.
type sourceGroup struct {
	name     string
	dir      string
	includes []string
	defines  []string
	skip     map[string]bool
	// skipPrefix skips any .c file whose name starts with this prefix.
	skipPrefix string
	// perFile maps filename to extra defines.
	perFile map[string][]string
}

func main() {
	goPath := flag.String("go", "go", "path to Go binary")
	outPath := flag.String("o", "build/libgastonc.a", "output archive path")
	testOnly := flag.Bool("test", false, "compile only, don't archive (test mode)")
	flag.Parse()

	// We run from the repo root but sources are relative to cmd/gaston.
	gastonDir := "cmd/gaston"

	groups := buildGroups()

	var objPaths []string
	passed, failed := 0, 0

	// Gaston .cm sources (libc/).
	cmDir := filepath.Join(gastonDir, "libc")
	entries, err := os.ReadDir(cmDir)
	if err != nil {
		fatalf("read %s: %v", cmDir, err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".cm") {
			continue
		}
		src := filepath.Join(cmDir, e.Name())
		obj := filepath.Join(os.TempDir(), fmt.Sprintf("gastonlibc-%s.o", strings.TrimSuffix(e.Name(), ".cm")))
		if err := compile(*goPath, gastonDir, src, obj, nil, nil); err != nil {
			fmt.Fprintf(os.Stderr, "FAIL %s: %v\n", e.Name(), err)
			failed++
			continue
		}
		objPaths = append(objPaths, obj)
		passed++
	}

	// Picolibc source groups.
	for _, g := range groups {
		dir := filepath.Join(gastonDir, g.dir)
		entries, err := os.ReadDir(dir)
		if err != nil {
			fatalf("read %s: %v", dir, err)
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".c") {
				continue
			}
			if g.skip[e.Name()] {
				continue
			}
			if g.skipPrefix != "" && strings.HasPrefix(e.Name(), g.skipPrefix) {
				continue
			}
			src := filepath.Join(dir, e.Name())
			obj := filepath.Join(os.TempDir(), fmt.Sprintf("gastonlibc-%s-%s.o", g.name, strings.TrimSuffix(e.Name(), ".c")))

			defines := append([]string{}, g.defines...)
			if extra, ok := g.perFile[e.Name()]; ok {
				defines = append(defines, extra...)
			}

			// Resolve include paths relative to gastonDir.
			includes := make([]string, len(g.includes))
			for i, inc := range g.includes {
				includes[i] = filepath.Join(gastonDir, inc)
			}

			if err := compile(*goPath, gastonDir, src, obj, includes, defines); err != nil {
				fmt.Fprintf(os.Stderr, "FAIL [%s] %s: %v\n", g.name, e.Name(), err)
				failed++
				continue
			}
			objPaths = append(objPaths, obj)
			passed++
		}
	}

	fmt.Fprintf(os.Stderr, "%d compiled, %d failed\n", passed, failed)

	if failed > 0 {
		os.Exit(1)
	}

	if *testOnly {
		// Clean up objects.
		for _, p := range objPaths {
			os.Remove(p)
		}
		return
	}

	// Archive.
	if err := archive(*goPath, *outPath, objPaths); err != nil {
		fatalf("archive: %v", err)
	}
	fmt.Fprintf(os.Stderr, "wrote %s (%d objects)\n", *outPath, len(objPaths))

	// Clean up objects.
	for _, p := range objPaths {
		os.Remove(p)
	}
}

func compile(goPath, gastonDir, src, obj string, includes []string, defines []string) error {
	args := []string{"tool", "gaston", "-c", "-o", obj}
	for _, inc := range includes {
		args = append(args, "-I", inc)
	}
	for _, d := range defines {
		args = append(args, "-D", d)
	}
	args = append(args, src)

	cmd := exec.Command(goPath, args...)
	cmd.Dir = "." // repo root
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%v: %s", err, out)
	}
	return nil
}

func archive(goPath, outPath string, objPaths []string) error {
	args := []string{"tool", "gaston", "-ar", "-o", outPath}
	args = append(args, objPaths...)
	cmd := exec.Command(goPath, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%v: %s", err, out)
	}
	return nil
}

func fatalf(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, "gastonlibc: "+format+"\n", args...)
	os.Exit(1)
}

func buildGroups() []sourceGroup {
	return []sourceGroup{
		{
			name: "tinystdio",
			dir:  "picolibc/libc/tinystdio",
			includes: []string{
				"picolibc/libc/tinystdio",
				"libm/include",
				"picolibc/libc/include",
			},
			defines: []string{"FORMAT_DEFAULT_DOUBLE=1"},
			skip: map[string]bool{
				"conv_flt.c": true, // template file #include'd by vfscanf.c
			},
			perFile: map[string][]string{
				"fdopen.c":   {"POSIX_IO"},
				"fmemopen.c": {"POSIX_IO"},
				"fopen.c":    {"POSIX_IO"},
				"freopen.c":  {"POSIX_IO"},
				"posixiob.c": {"POSIX_IO"},
				"vfscanf.c":  {"_WANT_IO_C99_FORMATS=1"},
				"vfscanff.c": {"_WANT_IO_C99_FORMATS=1"},
			},
		},
		{
			name: "string",
			dir:  "picolibc/libc/string",
			includes: []string{
				"picolibc/libc/string",
				"libm/include",
				"picolibc/libc/include",
			},
			defines: []string{"__SVID_VISIBLE=1", "__POSIX_VISIBLE=1", "__XSI_VISIBLE=1"},
			skip: map[string]bool{
				"strdup_r.c":       true, // calls _malloc_r
				"strndup_r.c":      true, // calls _malloc_r
				"strerror_r.c":     true, // calls _strerror_r
				"xpg_strerror_r.c": true, // calls _strerror_r
			},
		},
		{
			name: "ctype",
			dir:  "picolibc/libc/ctype",
			includes: []string{
				"picolibc/libc/ctype",
				"libm/include",
				"picolibc/libc/include",
				"picolibc/libc/locale",
			},
			defines: []string{"__PICOLIBC__=1", "_LIBC=1"},
		},
		{
			name: "search",
			dir:  "picolibc/libc/search",
			includes: []string{
				"picolibc/libc/search",
				"libm/include",
				"picolibc/libc/include",
			},
			defines: []string{"__PICOLIBC__=1", "_LIBC=1", "_SEARCH_PRIVATE=1"},
			skip: map[string]bool{
				"ndbm.c": true, // BSD db internals
			},
		},
		{
			name: "misc",
			dir:  "picolibc/libc/misc",
			includes: []string{
				"libm/include",
				"picolibc/libc/include",
			},
			defines: []string{"__PICOLIBC__=1", "_LIBC=1"},
		},
		{
			name: "argz",
			dir:  "picolibc/libc/argz",
			includes: []string{
				"libm/include",
				"picolibc/libc/include",
			},
			defines: []string{"__PICOLIBC__=1", "_LIBC=1"},
		},
		{
			name: "stdlib",
			dir:  "picolibc/libc/stdlib",
			includes: []string{
				"picolibc/libc/stdlib",
				"libm/include",
				"picolibc/libc/include",
			},
			defines: []string{
				"__SVID_VISIBLE=1", "__POSIX_VISIBLE=1", "__XSI_VISIBLE=1",
				"_LIBC=1", "__SINGLE_THREAD=1", "TINY_STDIO=1",
			},
			skip: map[string]bool{
				"calloc.c": true, // newlib reentrant wrapper; malloc-calloc.c provides calloc via mallocr.c
				"mtrim.c":  true, // newlib reentrant wrapper; malloc_trim provided by mallocr.c
			},
			skipPrefix: "nano-malloc", // exclude nano-malloc; we use dlmalloc (malloc-*.c / mallocr.c)
		},
		{
			name: "locale",
			dir:  "picolibc/libc/locale",
			includes: []string{
				"picolibc/libc/locale",
				"libm/include",
				"picolibc/libc/include",
			},
			defines: []string{"__SVID_VISIBLE=1", "__POSIX_VISIBLE=1", "__XSI_VISIBLE=1"},
		},
		{
			name: "posix",
			dir:  "picolibc/libc/posix",
			includes: []string{
				"picolibc/libc/posix",
				"libm/include",
				"picolibc/libc/include",
			},
			defines: []string{"__SVID_VISIBLE=1", "__POSIX_VISIBLE=1", "__XSI_VISIBLE=1"},
			skip: map[string]bool{
				"engine.c":  true, // template file
				"regexec.c": true, // regmatch_t array param mismatch
			},
		},
		{
			name: "signal",
			dir:  "picolibc/libc/signal",
			includes: []string{
				"picolibc/libc/signal",
				"libm/include",
				"picolibc/libc/include",
			},
			defines: []string{"__SVID_VISIBLE=1", "__POSIX_VISIBLE=1", "__XSI_VISIBLE=1"},
		},
		{
			name: "time",
			dir:  "picolibc/libc/time",
			includes: []string{
				"picolibc/libc/time",
				"libm/include",
				"picolibc/libc/include",
			},
			defines: []string{"__SVID_VISIBLE=1", "__POSIX_VISIBLE=1", "__XSI_VISIBLE=1"},
		},
		{
			name: "libm-math",
			dir:  "libm/math",
			includes: []string{
				"libm/common",
				"libm/include",
			},
		},
		{
			name: "libm-common",
			dir:  "libm/common",
			includes: []string{
				"libm/common",
				"libm/include",
			},
		},
	}
}
