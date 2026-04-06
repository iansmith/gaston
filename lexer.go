package main

import (
	"fmt"
	"math"
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
	anonCount   int              // counter for anonymous struct/union synthetic names
	typedefs        map[string]*CType   // typedef name → full CType (leaf or pointer)
	shadowedTypedefs map[string]int      // typedef names shadowed by local vars: name → brace depth when shadowed
	braceDepth       int                // current {} nesting depth (0 = global scope)
	typeofExpr      *Node              // scratch: holds typeof(expr) expression until declaration picks it up
	prevTok         int                // previous token returned (for struct/union tag disambiguation)
}

func newLexer(src, file string) *lexer {
	l := &lexer{
		src:      src,
		pos:      0,
		line:     1,
		file:     file,
		typedefs:        make(map[string]*CType),
		shadowedTypedefs: make(map[string]int),
	}
	// Pre-register "bool" as a typedef for _Bool (TypeInt).
	l.typedefs["bool"] = leafCType(TypeInt)
	// Pre-register wchar_t / wint_t as unsigned int (AArch64 LP64 ABI).
	l.typedefs["wchar_t"]  = leafCType(TypeUnsignedInt)
	l.typedefs["wint_t"]   = leafCType(TypeUnsignedInt)
	// Pre-register 128-bit integer typedefs.
	l.typedefs["__uint128_t"] = leafCType(TypeUint128)
	l.typedefs["__int128_t"]  = leafCType(TypeInt128)
	// Pre-register common C99/POSIX fixed-width and size types (LP64 AArch64).
	l.typedefs["size_t"]    = leafCType(TypeUnsignedLong)
	l.typedefs["ssize_t"]   = leafCType(TypeLong)
	l.typedefs["ptrdiff_t"] = leafCType(TypeLong)
	l.typedefs["intptr_t"]  = leafCType(TypeLong)
	l.typedefs["uintptr_t"] = leafCType(TypeUnsignedLong)
	l.typedefs["off_t"]     = leafCType(TypeLong)
	l.typedefs["off64_t"]   = leafCType(TypeLong)
	l.typedefs["uint8_t"]   = leafCType(TypeUnsignedChar)
	l.typedefs["uint16_t"]  = leafCType(TypeUnsignedShort)
	l.typedefs["uint32_t"]  = leafCType(TypeUnsignedInt)
	l.typedefs["uint64_t"]  = leafCType(TypeUnsignedLong)
	l.typedefs["int8_t"]    = leafCType(TypeChar)
	l.typedefs["int16_t"]   = leafCType(TypeShort)
	l.typedefs["int32_t"]   = leafCType(TypeInt)
	l.typedefs["int64_t"]   = leafCType(TypeLong)
	// picolibc internal fixed-width types.
	l.typedefs["__uint8_t"]  = leafCType(TypeUnsignedChar)
	l.typedefs["__uint16_t"] = leafCType(TypeUnsignedShort)
	l.typedefs["__uint32_t"] = leafCType(TypeUnsignedInt)
	l.typedefs["__uint64_t"] = leafCType(TypeUnsignedLong)
	l.typedefs["__int8_t"]   = leafCType(TypeChar)
	l.typedefs["__int16_t"]  = leafCType(TypeShort)
	l.typedefs["__int32_t"]  = leafCType(TypeInt)
	l.typedefs["__int64_t"]  = leafCType(TypeLong)
	// POSIX / picolibc supplemental types.
	l.typedefs["__ssize_t"]   = leafCType(TypeLong)
	l.typedefs["__blkcnt_t"]  = leafCType(TypeLong)
	l.typedefs["__clockid_t"] = leafCType(TypeUnsignedLong)
	l.typedefs["__timer_t"]   = leafCType(TypeUnsignedLong)
	l.typedefs["__pid_t"]     = leafCType(TypeInt)
	l.typedefs["__uid_t"]     = leafCType(TypeUnsignedInt)
	l.typedefs["__gid_t"]     = leafCType(TypeUnsignedInt)
	l.typedefs["__dev_t"]     = leafCType(TypeUnsignedLong)
	l.typedefs["__ino_t"]     = leafCType(TypeUnsignedLong)
	l.typedefs["__mode_t"]    = leafCType(TypeUnsignedInt)
	l.typedefs["__nlink_t"]   = leafCType(TypeUnsignedInt)
	l.typedefs["__off_t"]     = leafCType(TypeLong)
	l.typedefs["__off64_t"]   = leafCType(TypeLong)
	l.typedefs["__blksize_t"] = leafCType(TypeLong)
	l.typedefs["__time_t"]    = leafCType(TypeLong)
	l.typedefs["__suseconds_t"] = leafCType(TypeLong)
	l.typedefs["__useconds_t"]  = leafCType(TypeUnsignedInt)
	l.typedefs["__socklen_t"]   = leafCType(TypeUnsignedInt)
	l.typedefs["__float64"]   = leafCType(TypeDouble)
	l.typedefs["__float32"]   = leafCType(TypeFloat)
	// tinystdio internal type: ultoa_unsigned_t is unsigned long long on LP64.
	l.typedefs["ultoa_unsigned_t"] = leafCType(TypeUnsignedLong)
	return l
}

// nextAnon returns the next synthetic anonymous struct/union tag name.
func (l *lexer) nextAnon() string {
	name := fmt.Sprintf("__anon_%d", l.anonCount)
	l.anonCount++
	return name
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

// lookupConstInt looks up a named integer constant for use in const_int_expr
// array dimension evaluation. Returns 1 for unknown names (safe non-zero default).
func (l *lexer) lookupConstInt(name string) int {
	_ = name
	return 1 // opaque: preprocessor macros are expanded before parse; any remaining ID is unknown
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
	"switch":   SWITCH,
	"case":     CASE,
	"default":  DEFAULT,
	"_Bool":      INT,
	"va_arg":     VA_ARG,
	"typeof":      TYPEOF,
	"__typeof":    TYPEOF,
	"__typeof__":  TYPEOF,
	"__int128":    INT128,
	"__int128_t":  INT128,
	"signed":      SIGNED,
}

// skipWords lists storage-class and qualifier keywords that the lexer silently drops.
var skipWords = map[string]bool{
	// Standard qualifiers / storage classes
	"volatile":   true,
	"register":   true,
	"restrict":   true,
	"inline":     true,
	// C11 function specifiers
	"_Noreturn":  true,
	"noreturn":   true,
	// GCC alternate spellings (appear after __GNUC__=4 cdefs.h expansion)
	"__restrict__":  true,
	"__volatile__":  true,
	"__const__":     true,
	"__signed__":    true,
	"__inline__":    true,
	"__inline":      true,
	"__noreturn__":  true,
	"__extension__": true,
	// _Alignas / _Alignof are silently dropped (we don't enforce alignment)
	"_Alignas":   true,
	"_Alignof":   true,
	// Thread-local storage specifier (GCC __thread / C11 _Thread_local)
	"__thread":       true,
	"_Thread_local":  true,
}

// Lex scans and returns the next token, filling lval with the token's value.
// Returns 0 on EOF.
func (l *lexer) Lex(lval *yySymType) int {
	tok := l.lex(lval)
	l.prevTok = tok
	// Track brace depth for typedef-shadow scoping.
	if tok == int('{') {
		l.braceDepth++
	} else if tok == int('}') {
		// Unshadow any typedef names that were shadowed at this depth.
		for name, depth := range l.shadowedTypedefs {
			if depth >= l.braceDepth {
				delete(l.shadowedTypedefs, name)
			}
		}
		if l.braceDepth > 0 {
			l.braceDepth--
		}
	}
	return tok
}

func (l *lexer) lex(lval *yySymType) int {
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
		if l.pos < len(l.src) && (l.src[l.pos] == 'f' || l.src[l.pos] == 'F' || l.src[l.pos] == 'l' || l.src[l.pos] == 'L') {
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
			// Check for hex-float: 0x<hex>.<hex>p<sign><dec> (C99 §6.4.4.2)
			if l.pos < len(l.src) && (l.src[l.pos] == '.' || l.src[l.pos] == 'p' || l.src[l.pos] == 'P') {
				// optional fractional part
				if l.src[l.pos] == '.' {
					l.pos++
					for l.pos < len(l.src) && isHexDigit(l.src[l.pos]) {
						l.pos++
					}
				}
				// required binary exponent: p or P, optional sign, decimal digits
				if l.pos < len(l.src) && (l.src[l.pos] == 'p' || l.src[l.pos] == 'P') {
					l.pos++
					if l.pos < len(l.src) && (l.src[l.pos] == '+' || l.src[l.pos] == '-') {
						l.pos++
					}
					for l.pos < len(l.src) && isDigit(l.src[l.pos]) {
						l.pos++
					}
				}
				end := l.pos
				// optional f/F suffix (treat as float64)
				if l.pos < len(l.src) && (l.src[l.pos] == 'f' || l.src[l.pos] == 'F') {
					l.pos++
				}
				v, err := strconv.ParseFloat(l.src[start:end], 64)
				if err != nil {
					l.Error(fmt.Sprintf("invalid hex float literal: %s", l.src[start:end]))
					return 0
				}
				lval.fval = v
				return FNUM
			}
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
			l.pos++ // consume f/F suffix
		} else if isFloat && l.pos < len(l.src) && (l.src[l.pos] == 'l' || l.src[l.pos] == 'L') {
			l.pos++ // consume l/L long-double suffix (already a float)
		}
		if isFloat {
			s := l.src[start:end]
			s = strings.TrimSuffix(s, "f")
			s = strings.TrimSuffix(s, "F")
			v, err := strconv.ParseFloat(s, 64)
			if err != nil {
				// Overflow (e.g. 1e+512L) → treat as +Inf; sign-flip handled elsewhere.
				if numErr, ok := err.(*strconv.NumError); ok && numErr.Err == strconv.ErrRange {
					if strings.HasPrefix(s, "-") {
						v = math.Inf(-1)
					} else {
						v = math.Inf(1)
					}
				} else {
					l.Error(fmt.Sprintf("invalid float literal: %s", l.src[start:l.pos]))
					return 0
				}
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
			// Try parsing as unsigned 64-bit (for values > math.MaxInt64 with 'u' suffix).
			uv, uerr := strconv.ParseUint(l.src[start:numEnd], 0, 64)
			if uerr != nil {
				l.Error(fmt.Sprintf("invalid integer literal: %s", l.src[start:numEnd]))
				return 0
			}
			lval.ival = int(int64(uv)) // reinterpret bit pattern as signed
		} else {
			lval.ival = int(v)
		}
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
			case 'a':
				val = '\a'
			case 'b':
				val = '\b'
			case 'f':
				val = '\f'
			case 'v':
				val = '\v'
			case '\\':
				val = '\\'
			case '\'':
				val = '\''
			case '"':
				val = '"'
			case 'x', 'X':
				// Hex escape: \xNN
				l.pos++
				hex := 0
				for l.pos < len(l.src) && isHexDigit(l.src[l.pos]) {
					hex = hex*16 + hexVal(l.src[l.pos])
					l.pos++
				}
				val = hex
				l.pos-- // will be incremented after switch
			case '0', '1', '2', '3', '4', '5', '6', '7':
				// Octal escape: \0NN or \NNN (1-3 octal digits)
				oct := int(l.src[l.pos] - '0')
				l.pos++
				for count := 0; count < 2 && l.pos < len(l.src) && l.src[l.pos] >= '0' && l.src[l.pos] <= '7'; count++ {
					oct = oct*8 + int(l.src[l.pos]-'0')
					l.pos++
				}
				val = oct
				l.pos-- // will be incremented after switch
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
				case 'a':
					buf = append(buf, '\a')
				case 'b':
					buf = append(buf, '\b')
				case 'f':
					buf = append(buf, '\f')
				case 'v':
					buf = append(buf, '\v')
				case '\\':
					buf = append(buf, '\\')
				case '\'':
					buf = append(buf, '\'')
				case '"':
					buf = append(buf, '"')
				case 'x', 'X':
					// Hex escape: \xNN
					l.pos++
					hex := 0
					for l.pos < len(l.src) && isHexDigit(l.src[l.pos]) {
						hex = hex*16 + hexVal(l.src[l.pos])
						l.pos++
					}
					buf = append(buf, byte(hex))
					continue // l.pos already past the hex digits
				case '0', '1', '2', '3', '4', '5', '6', '7':
					// Octal escape: \NNN (1-3 octal digits)
					oct := int(l.src[l.pos] - '0')
					l.pos++
					for count := 0; count < 2 && l.pos < len(l.src) && l.src[l.pos] >= '0' && l.src[l.pos] <= '7'; count++ {
						oct = oct*8 + int(l.src[l.pos]-'0')
						l.pos++
					}
					buf = append(buf, byte(oct))
					continue // l.pos already past the octal digits
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
		// Adjacent string literal concatenation: "a" "b" → "ab"
		for {
			j := l.pos
			for j < len(l.src) && (l.src[j] == ' ' || l.src[j] == '\t' || l.src[j] == '\r' || l.src[j] == '\n') {
				if l.src[j] == '\n' {
					l.line++
				}
				j++
			}
			if j >= len(l.src) || l.src[j] != '"' {
				break
			}
			l.pos = j + 1 // consume opening quote of next string
			for l.pos < len(l.src) && l.src[l.pos] != '"' {
				ch := l.src[l.pos]
				if ch == '\n' {
					l.Error("newline in string literal")
					break
				}
				if ch == '\\' {
					l.pos++
					if l.pos >= len(l.src) {
						break
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
						buf = append(buf, l.src[l.pos])
					}
					l.pos++
				} else {
					buf = append(buf, ch)
					l.pos++
				}
			}
			if l.pos < len(l.src) {
				l.pos++ // consume closing quote
			}
		}
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
		// Exception 1: after struct/union keyword the tag is always an ID, even if
		// the same name was also declared as a typedef (e.g. typedef struct FILE FILE).
		// Exception 2: after a TYPENAME token, we're in a declarator position (e.g.
		// "uint64_t uint;" where "uint" is both a typedef and a variable name); treat
		// the identifier as ID so the declaration grammar rule matches.
		if _, ok := l.typedefs[word]; ok && l.prevTok != STRUCT && l.prevTok != UNION {
			if l.prevTok == TYPENAME {
				// "uint64_t uint;" — typedef name used as variable name after another type.
				// Shadow it at the current brace depth so future uses inside this
				// scope return ID; the shadow is cleared when its brace depth closes.
				if l.braceDepth > 0 {
					l.shadowedTypedefs[word] = l.braceDepth
				}
				return ID // redeclaring a typedef name as a variable — treat as ID
			}
			// If this typedef name has been shadowed by a local variable declaration
			// in the current scope (or an enclosing local scope), treat it as ID.
			if _, shadowed := l.shadowedTypedefs[word]; shadowed {
				return ID
			}
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
	case '?':
		return QUESTION
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
	ctx := ""
	if l.pos < len(l.src) {
		start := l.pos - 60
		if start < 0 {
			start = 0
		}
		end := l.pos + 40
		if end > len(l.src) {
			end = len(l.src)
		}
		before := strings.ReplaceAll(l.src[start:l.pos], "\n", " ")
		after := strings.ReplaceAll(l.src[l.pos:end], "\n", " ")
		ctx = fmt.Sprintf(" [before: %q after: %q]", before, after)
	}
	fmt.Fprintf(os.Stderr, "%s:%d: %s%s\n", l.file, l.line, s, ctx)
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

func hexVal(c byte) int {
	switch {
	case c >= '0' && c <= '9':
		return int(c - '0')
	case c >= 'a' && c <= 'f':
		return int(c-'a') + 10
	default:
		return int(c-'A') + 10
	}
}
