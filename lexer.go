package main

import (
	"fmt"
	"os"
)

// lexer implements the yyLexer interface required by the goyacc-generated parser.
type lexer struct {
	src    string
	pos    int
	line   int
	file   string
	errors int
	result *Node // set by the top-level grammar action
}

func newLexer(src, file string) *lexer {
	return &lexer{src: src, pos: 0, line: 1, file: file}
}

// keywords maps reserved words to their goyacc token constants.
var keywords = map[string]int{
	"int":    INT,
	"void":   VOID,
	"if":     IF,
	"else":   ELSE,
	"while":  WHILE,
	"return": RETURN,
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

	// Integer literals.
	if isDigit(c) {
		v := 0
		for l.pos < len(l.src) && isDigit(l.src[l.pos]) {
			v = v*10 + int(l.src[l.pos]-'0')
			l.pos++
		}
		lval.ival = v
		return NUM
	}

	// Identifiers and keywords.
	if isLetter(c) {
		start := l.pos
		for l.pos < len(l.src) && (isLetter(l.src[l.pos]) || isDigit(l.src[l.pos])) {
			l.pos++
		}
		word := l.src[start:l.pos]
		if tok, ok := keywords[word]; ok {
			return tok
		}
		lval.sval = word
		return ID
	}

	// Single- and double-character punctuation / operators.
	l.pos++
	switch c {
	case '<':
		if l.pos < len(l.src) && l.src[l.pos] == '=' {
			l.pos++
			return LE
		}
		return int('<')
	case '>':
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
	case '!':
		if l.pos < len(l.src) && l.src[l.pos] == '=' {
			l.pos++
			return NE
		}
		l.Error("unexpected '!'")
		return 0
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
