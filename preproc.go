package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
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

// builtinHeaders provides virtual content for standard headers that gaston
// implements internally rather than relying on a host system libc.
var builtinHeaders = map[string]string{
	"stdarg.h": `
/* gaston built-in <stdarg.h> */
typedef long* va_list;
#define va_start(ap, last)  ap = __va_start()
#define va_end(ap)
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

// newPreprocessor creates a preprocessor with the given include search paths.
// The gaston libc directory ("libc") is always appended as the final search
// directory so that #include <stdarg.h>, <stddef.h>, etc. resolve to the
// real header files when running from the cmd/gaston working directory.
func newPreprocessor(includePaths []string) *preprocessor {
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
	return &preprocessor{
		defines:      make(map[string]*macroDef),
		includePaths: paths,
		inInclude:    make(map[string]bool),
	}
}

// Preprocess runs the preprocessor on src (source file name file) and returns
// the expanded text, ready for lexing.
func (p *preprocessor) Preprocess(src, file string) (string, error) {
	var out strings.Builder
	p.processFile(src, file, &out)
	if p.errors > 0 {
		return "", fmt.Errorf("preprocessor: %d error(s)", p.errors)
	}
	return out.String(), nil
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

	for _, ll := range splitLogical(src) {
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

			case "include":
				if active {
					p.processInclude(rest, file, lineNum, out)
				}

			default:
				if active {
					p.errorf(file, lineNum, "unknown directive #%s", dir)
				}
			}
			// Directive lines produce no code output — emit blank lines so the
			// lexer's line numbers stay aligned with the original source.
			for i := 0; i < ll.count; i++ {
				out.WriteByte('\n')
			}
		} else if active {
			out.WriteString(p.expandLine(ll.text))
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
		p.processFile(content, "<"+filename+">", out)
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
	p.processFile(string(data), fullPath, out)
}

// expandLine expands macros in one logical line, skipping string/char literals
// and line comments.  Multiple passes are performed until the output stabilises
// or a depth limit is reached (guards against unterminated expansion chains).
func (p *preprocessor) expandLine(line string) string {
	const maxPasses = 32
	for pass := 0; pass < maxPasses; pass++ {
		next := p.expandLineOnce(line)
		if next == line {
			return line
		}
		line = next
	}
	return line
}

func (p *preprocessor) expandLineOnce(line string) string {
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

			if def == nil {
				// Not a macro.
				out.WriteString(name)
				i = j
				continue
			}

			if def.params == nil {
				// Object-like macro.
				out.WriteString(def.body)
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
			// Pre-expand each argument before substitution (C standard behaviour).
			for ai, a := range args {
				args[ai] = p.expandLine(a)
			}
			out.WriteString(p.applyFuncMacro(def, name, args))
			i = end
			continue
		}

		out.WriteByte(c)
		i++
	}
	return out.String()
}

// applyFuncMacro substitutes actual arguments into a function-like macro body.
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

	var out strings.Builder
	body := def.body
	i := 0
	for i < len(body) {
		if isLetter(body[i]) {
			j := i + 1
			for j < len(body) && (isLetter(body[j]) || isDigit(body[j])) {
				j++
			}
			tok := body[i:j]

			if tok == "__VA_ARGS__" && def.variadic {
				varArgs := args[len(def.params):]
				out.WriteString(strings.Join(varArgs, ", "))
				i = j
				continue
			}
			replaced := false
			for idx, param := range def.params {
				if tok == param {
					if idx < len(args) {
						out.WriteString(args[idx])
					}
					replaced = true
					break
				}
			}
			if !replaced {
				out.WriteString(tok)
			}
			i = j
			continue
		}
		out.WriteByte(body[i])
		i++
	}
	return out.String()
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
			buf.WriteString(line[:len(line)-1])
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
