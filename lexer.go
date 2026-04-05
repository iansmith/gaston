package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// lexer implements the yyLexer interface required by the goyacc-generated parser.
type lexer struct {
	src         string
	pos         int
	line        int
	file        string
	errors      int
	result      *Node            // set by the top-level grammar action
	enumAutoVal int              // auto-increment counter for enum constants
	typedefs    map[string]*CType // typedef name → full CType (leaf or pointer)
}

func newLexer(src, file string) *lexer {
	l := &lexer{
		src:      src,
		pos:      0,
		line:     1,
		file:     file,
		typedefs: make(map[string]*CType),
	}
	// Pre-register "bool" as a typedef for _Bool (TypeInt).
	// This lets "typedef _Bool bool;" work without "bool" being re-lexed as INT.
	l.typedefs["bool"] = leafCType(TypeInt)
	return l
}

// registerTypedef registers a typedef name with its full CType.
func (l *lexer) registerTypedef(name string, ct *CType) {
	l.typedefs[name] = ct
}

// lookupTypedefCType returns the full *CType registered for a typedef name.
// Returns a leaf TypeInt CType if the name is not registered.
func (l *lexer) lookupTypedefCType(name string) *CType {
	if ct, ok := l.typedefs[name]; ok {
		return ct
	}
	return leafCType(TypeInt)
}

// keywords maps reserved words to their goyacc token constants.
var keywords = map[string]int{
	"int":      INT,
	"void":     VOID,
	"if":       IF,
	"else":     ELSE,
	"while":    WHILE,
	"return":   RETURN,
	"for":      FOR,
	"do":       DO,
	"break":    BREAK,
	"continue": CONTINUE,
	"const":    CONST,
	"char":     CHAR,
	"extern":   EXTERN,
	"long":     LONG,
	"unsigned": UNSIGNED,
	"short":    SHORT,
	"float":    FLOAT,
	"double":   DOUBLE,
	"struct":   STRUCT,
	"goto":     GOTO,
	"sizeof":   SIZEOF,
	"enum":     ENUM,
	"union":    UNION,
	"typedef":  TYPEDEF,
	"static":   STATIC,
	"_Bool":    INT,
	"va_arg":   VA_ARG,
}

// skipWords lists storage-class and qualifier keywords that the lexer silently drops.
var skipWords = map[string]bool{
	"volatile": true,
	"register": true,
	"restrict": true,
	"inline":   true,
}

// Lex scans and returns the next token, filling lval with the token's value.
// Returns 0 on EOF.
func (l *lexer) Lex(lval *yySymType) int {
	// Skip whitespace and block comments.
	for l.pos < len(l.src) {
		c := l.src[l.pos]
		switch {
		case c == ' ' || c == '\t' || c == '\r':
			l.pos++
		case c == '\n':
			l.line++
			l.pos++
		case c == '/' && l.pos+1 < len(l.src) && l.src[l.pos+1] == '*':
			if !l.skipComment() {
				return 0
			}
		default:
			goto scan
		}
	}
	return 0 // EOF

scan:
	c := l.src[l.pos]

	// Float literal starting with '.': e.g. .5, .25e-3
	if c == '.' && l.pos+1 < len(l.src) && isDigit(l.src[l.pos+1]) {
		start := l.pos
		l.pos++ // consume '.'
		for l.pos < len(l.src) && isDigit(l.src[l.pos]) {
			l.pos++
		}
		end := l.pos
		if l.pos < len(l.src) && (l.src[l.pos] == 'e' || l.src[l.pos] == 'E') {
			l.pos++
			if l.pos < len(l.src) && (l.src[l.pos] == '+' || l.src[l.pos] == '-') {
				l.pos++
			}
			for l.pos < len(l.src) && isDigit(l.src[l.pos]) {
				l.pos++
			}
			end = l.pos
		}
		if l.pos < len(l.src) && (l.src[l.pos] == 'f' || l.src[l.pos] == 'F') {
			l.pos++ // consume suffix but exclude from parse
		}
		v, err := strconv.ParseFloat(l.src[start:end], 64)
		if err != nil {
			l.Error(fmt.Sprintf("invalid float literal: %s", l.src[start:l.pos]))
			return 0
		}
		lval.fval = v
		return FNUM
	}

	// Integer literals: decimal, hex (0x/0X), octal (0…).
	if isDigit(c) {
		start := l.pos
		if c == '0' && l.pos+1 < len(l.src) && (l.src[l.pos+1] == 'x' || l.src[l.pos+1] == 'X') {
			l.pos += 2 // skip 0x
			for l.pos < len(l.src) && isHexDigit(l.src[l.pos]) {
				l.pos++
			}
			numEnd := l.pos
			// Consume and discard any integer suffixes (u, U, l, L).
			for l.pos < len(l.src) && (l.src[l.pos] == 'u' || l.src[l.pos] == 'U' || l.src[l.pos] == 'l' || l.src[l.pos] == 'L') {
				l.pos++
			}
			// Hex literals are always integers.
			v, err := strconv.ParseInt(l.src[start:numEnd], 0, 64)
			if err != nil {
				// Try unsigned parse for values > int64 max (e.g. 0xFFFFFFFFFFFFFFFF).
				uv, uerr := strconv.ParseUint(l.src[start:numEnd], 0, 64)
				if uerr != nil {
					l.Error(fmt.Sprintf("invalid integer literal: %s", l.src[start:numEnd]))
					return 0
				}
				lval.ival = int(int64(uv))
				return NUM
			}
			lval.ival = int(v)
			return NUM
		}
		// Decimal: scan digits first.
		for l.pos < len(l.src) && isDigit(l.src[l.pos]) {
			l.pos++
		}
		// Check for float indicators: decimal point or exponent.
		isFloat := false
		end := l.pos
		if l.pos < len(l.src) && l.src[l.pos] == '.' {
			isFloat = true
			l.pos++ // consume '.'
			for l.pos < len(l.src) && isDigit(l.src[l.pos]) {
				l.pos++
			}
			end = l.pos
		}
		if l.pos < len(l.src) && (l.src[l.pos] == 'e' || l.src[l.pos] == 'E') {
			isFloat = true
			l.pos++
			if l.pos < len(l.src) && (l.src[l.pos] == '+' || l.src[l.pos] == '-') {
				l.pos++
			}
			for l.pos < len(l.src) && isDigit(l.src[l.pos]) {
				l.pos++
			}
			end = l.pos
		}
		if l.pos < len(l.src) && (l.src[l.pos] == 'f' || l.src[l.pos] == 'F') {
			isFloat = true
			l.pos++ // consume suffix but exclude from numeric parse
		}
		if isFloat {
			s := l.src[start:end]
			s = strings.TrimSuffix(s, "f")
			s = strings.TrimSuffix(s, "F")
			v, err := strconv.ParseFloat(s, 64)
			if err != nil {
				l.Error(fmt.Sprintf("invalid float literal: %s", l.src[start:l.pos]))
				return 0
			}
			lval.fval = v
			return FNUM
		}
		numEnd := l.pos
		// Consume and discard any integer suffixes (u, U, l, L).
		for l.pos < len(l.src) && (l.src[l.pos] == 'u' || l.src[l.pos] == 'U' || l.src[l.pos] == 'l' || l.src[l.pos] == 'L') {
			l.pos++
		}
		v, err := strconv.ParseInt(l.src[start:numEnd], 0, 64)
		if err != nil {
			l.Error(fmt.Sprintf("invalid integer literal: %s", l.src[start:numEnd]))
			return 0
		}
		lval.ival = int(v)
		return NUM
	}

	// Character literals: 'x' or '\n' etc.
	if c == '\'' {
		l.pos++ // consume opening quote
		if l.pos >= len(l.src) {
			l.Error("unterminated character literal")
			return 0
		}
		var val int
		if l.src[l.pos] == '\\' {
			l.pos++
			if l.pos >= len(l.src) {
				l.Error("unterminated escape in char literal")
				return 0
			}
			switch l.src[l.pos] {
			case 'n':
				val = '\n'
			case 't':
				val = '\t'
			case 'r':
				val = '\r'
			case '0':
				val = 0
			case '\\':
				val = '\\'
			case '\'':
				val = '\''
			default:
				l.Error(fmt.Sprintf("unknown escape '\\%c'", l.src[l.pos]))
			}
			l.pos++
		} else {
			val = int(l.src[l.pos])
			l.pos++
		}
		if l.pos >= len(l.src) || l.src[l.pos] != '\'' {
			l.Error("unterminated character literal")
			return 0
		}
		l.pos++ // consume closing quote
		lval.ival = val
		return CHAR_LIT
	}

	// String literals: "...".
	if c == '"' {
		l.pos++ // consume opening quote
		start := l.pos
		var buf []byte
		for l.pos < len(l.src) && l.src[l.pos] != '"' {
			ch := l.src[l.pos]
			if ch == '\n' {
				l.Error("newline in string literal")
				return 0
			}
			if ch == '\\' {
				l.pos++
				if l.pos >= len(l.src) {
					l.Error("unterminated escape in string literal")
					return 0
				}
				switch l.src[l.pos] {
				case 'n':
					buf = append(buf, '\n')
				case 't':
					buf = append(buf, '\t')
				case 'r':
					buf = append(buf, '\r')
				case '0':
					buf = append(buf, 0)
				case '\\':
					buf = append(buf, '\\')
				case '"':
					buf = append(buf, '"')
				default:
					l.Error(fmt.Sprintf("unknown escape '\\%c'", l.src[l.pos]))
				}
				l.pos++
			} else {
				buf = append(buf, ch)
				l.pos++
			}
			_ = start
		}
		if l.pos >= len(l.src) {
			l.Error("unterminated string literal")
			return 0
		}
		l.pos++ // consume closing quote
		lval.sval = string(buf)
		return STRING_LIT
	}

	// Identifiers and keywords.
	if isLetter(c) {
		start := l.pos
		for l.pos < len(l.src) && (isLetter(l.src[l.pos]) || isDigit(l.src[l.pos])) {
			l.pos++
		}
		word := l.src[start:l.pos]
		// Silently skip storage-class/qualifier keywords (volatile, register, restrict, inline).
		if skipWords[word] {
			return l.Lex(lval) // re-enter to skip whitespace and get next token
		}
		if tok, ok := keywords[word]; ok {
			return tok
		}
		lval.sval = word
		// If this identifier was registered as a typedef name, return TYPENAME.
		if _, ok := l.typedefs[word]; ok {
			return TYPENAME
		}
		return ID
	}

	// Single- and two-character operators.
	l.pos++
	switch c {
	case '+':
		if l.pos < len(l.src) && l.src[l.pos] == '+' {
			l.pos++
			return INC
		}
		if l.pos < len(l.src) && l.src[l.pos] == '=' {
			l.pos++
			return PLUSEQ
		}
		return int('+')
	case '.':
		if l.pos+1 < len(l.src) && l.src[l.pos] == '.' && l.src[l.pos+1] == '.' {
			l.pos += 2
			return ELLIPSIS
		}
		return int('.')
	case '-':
		if l.pos < len(l.src) && l.src[l.pos] == '>' {
			l.pos++
			return ARROW
		}
		if l.pos < len(l.src) && l.src[l.pos] == '-' {
			l.pos++
			return DEC
		}
		if l.pos < len(l.src) && l.src[l.pos] == '=' {
			l.pos++
			return MINUSEQ
		}
		return int('-')
	case '*':
		if l.pos < len(l.src) && l.src[l.pos] == '=' {
			l.pos++
			return STAREQ
		}
		return int('*')
	case '/':
		if l.pos < len(l.src) && l.src[l.pos] == '=' {
			l.pos++
			return DIVEQ
		}
		return int('/')
	case '%':
		if l.pos < len(l.src) && l.src[l.pos] == '=' {
			l.pos++
			return MODEQ
		}
		return int('%')
	case '<':
		if l.pos < len(l.src) && l.src[l.pos] == '<' {
			l.pos++
			if l.pos < len(l.src) && l.src[l.pos] == '=' {
				l.pos++
				return SHLEQ
			}
			return LSHIFT
		}
		if l.pos < len(l.src) && l.src[l.pos] == '=' {
			l.pos++
			return LE
		}
		return int('<')
	case '>':
		if l.pos < len(l.src) && l.src[l.pos] == '>' {
			l.pos++
			if l.pos < len(l.src) && l.src[l.pos] == '=' {
				l.pos++
				return SHREQ
			}
			return RSHIFT
		}
		if l.pos < len(l.src) && l.src[l.pos] == '=' {
			l.pos++
			return GE
		}
		return int('>')
	case '=':
		if l.pos < len(l.src) && l.src[l.pos] == '=' {
			l.pos++
			return EQ
		}
		return int('=')
	case '&':
		if l.pos < len(l.src) && l.src[l.pos] == '&' {
			l.pos++
			return ANDAND
		}
		if l.pos < len(l.src) && l.src[l.pos] == '=' {
			l.pos++
			return ANDEQ
		}
		return int('&')
	case '|':
		if l.pos < len(l.src) && l.src[l.pos] == '|' {
			l.pos++
			return OROR
		}
		if l.pos < len(l.src) && l.src[l.pos] == '=' {
			l.pos++
			return OREQ
		}
		return int('|')
	case '^':
		if l.pos < len(l.src) && l.src[l.pos] == '=' {
			l.pos++
			return XOREQ
		}
		return int('^')
	case '!':
		if l.pos < len(l.src) && l.src[l.pos] == '=' {
			l.pos++
			return NE
		}
		return int('!')
	default:
		return int(c)
	}
}

// skipComment advances past a /* ... */ block comment.
// Returns false if the comment is unterminated.
func (l *lexer) skipComment() bool {
	l.pos += 2 // consume /*
	for l.pos+1 < len(l.src) {
		if l.src[l.pos] == '\n' {
			l.line++
		}
		if l.src[l.pos] == '*' && l.src[l.pos+1] == '/' {
			l.pos += 2
			return true
		}
		l.pos++
	}
	l.Error("unterminated block comment")
	return false
}

// Error satisfies yyLexer; it is called by the parser on syntax errors.
func (l *lexer) Error(s string) {
	fmt.Fprintf(os.Stderr, "%s:%d: %s\n", l.file, l.line, s)
	l.errors++
}

func isLetter(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || c == '_'
}

func isDigit(c byte) bool {
	return c >= '0' && c <= '9'
}

func isHexDigit(c byte) bool {
	return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
}
