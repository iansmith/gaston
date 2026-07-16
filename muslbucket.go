package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// muslBucketReport summarizes a baseline compile sweep over a C source tree:
// every .c file is compiled in-process and failures are grouped into buckets
// by normalized error class. The bucket dashboard is the progress metric for
// the musl effort (design/musl-support-plan.md M1).
type muslBucketReport struct {
	Total  int
	Passed int
	Bucket map[string][]string // error class → files
}

var (
	bucketQuoted  = regexp.MustCompile(`(['"])[^'"]*(['"])`)
	bucketNumbers = regexp.MustCompile(`\b\d+\b`)
	bucketPathPfx = regexp.MustCompile(`(^|\s)\S+\.(c|h):(\d+:)*\s*`)
	bucketAfter   = regexp.MustCompile(`after: "([^"]*)"`)
	bucketToken   = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*|^[0-9]+|^[^\sA-Za-z0-9_]{1,2}`)
)

// bucketClass normalizes an error message into a bucket key: quoted tokens,
// numbers, and file:line prefixes are collapsed so that instances of the
// same defect land in one bucket.
func bucketClass(stage, msg string) string {
	// goyacc reports every parse failure as the same "syntax error" string;
	// the discriminating signal is the source context — key parse buckets on
	// the token at the error point instead.
	if stage == "parse" && strings.HasPrefix(msg, "syntax error") {
		if m := bucketAfter.FindStringSubmatch(msg); m != nil {
			at := strings.TrimSpace(m[1])
			if tok := bucketToken.FindString(at); tok != "" {
				return "parse: syntax error at '" + tok + "'"
			}
		}
		return "parse: syntax error"
	}
	msg = strings.SplitN(msg, "\n", 2)[0]
	msg = bucketPathPfx.ReplaceAllString(msg, " ")
	msg = bucketQuoted.ReplaceAllString(msg, "'…'")
	msg = bucketNumbers.ReplaceAllString(msg, "N")
	msg = strings.TrimSpace(msg)
	if len(msg) > 90 {
		msg = msg[:90] + "…"
	}
	return stage + ": " + msg
}

// compileOneForBucket runs the full in-process pipeline (preprocess, parse,
// semcheck, IR, objgen) on one file, returning "" on success or the bucket
// class of the first failure. Panics anywhere in the pipeline are recovered
// and bucketed.
func compileOneForBucket(srcPath string, includes []string, defines []string) (class string) {
	defer func() {
		if r := recover(); r != nil {
			class = bucketClass("panic", fmt.Sprint(r))
		}
	}()

	raw, err := os.ReadFile(srcPath)
	if err != nil {
		return bucketClass("read", err.Error())
	}
	pp := newPreprocessor(includes, defines)
	src, err := pp.Preprocess(string(raw), srcPath)
	if err != nil {
		return bucketClass("preprocess", err.Error())
	}
	lex := newLexer(src, srcPath)
	yyParse(lex)
	if lex.errors > 0 {
		return bucketClass("parse", lex.firstError)
	}
	if lex.result == nil {
		return bucketClass("parse", "empty program")
	}
	if err := semCheck(lex.result, false); err != nil {
		return bucketClass("semcheck", err.Error())
	}
	irp := genIR(lex.result)
	if _, err := genObjectBytes(irp); err != nil {
		return bucketClass("objgen", err.Error())
	}
	return ""
}

// muslCompileBaseline compiles every .c file under srcRoot with the given
// include paths and defines, recovering from per-file panics, and returns
// the bucketed report.
func muslCompileBaseline(srcRoot string, includes []string, defines []string) *muslBucketReport {
	rep := &muslBucketReport{Bucket: map[string][]string{}}
	filepath.WalkDir(srcRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".c") {
			return nil
		}
		rep.Total++
		if class := compileOneForBucket(path, includes, defines); class != "" {
			rep.Bucket[class] = append(rep.Bucket[class], path)
		} else {
			rep.Passed++
		}
		return nil
	})
	return rep
}
