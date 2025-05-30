/*
Copyright 2025 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package codetags

import (
	"bytes"
	"fmt"
	"iter"
	"strings"
	"unicode"
)

type token struct {
	r     rune
	pos   int
	value string
}

func runeString(r rune) string {
	var str string
	switch r {
	case NUMBER:
		str = "NUMBER"
	case IDENTIFIER:
		str = "IDENTIFIER"
	case BOOLEAN:
		str = "BOOLEAN"
	case STRING:
		str = "STRING"
	case TAG_NAME:
		str = "TAG_NAME"
	case EOF:
		str = "EOF"
	case ERROR:
		str = "ERROR"
	default:
		return fmt.Sprintf("unknown token: %v", r)
	}
	return fmt.Sprintf("%s", str)
}

func toArgType(r rune) ArgType {
	switch r {
	case NUMBER:
		return ArgTypeInt
	case IDENTIFIER:
		return ArgTypeString
	case BOOLEAN:
		return ArgTypeBool
	case STRING:
		return ArgTypeString
	default:
		return "unknown"
	}
}

func toValueType(r rune) ValueType {
	switch r {
	case NUMBER:
		return ValueTypeInt
	case IDENTIFIER:
		return ValueTypeString
	case BOOLEAN:
		return ValueTypeBool
	case STRING:
		return ValueTypeString
	default:
		return "unknown"
	}
}

func runesString(runes []rune) string {
	var result strings.Builder
	for i, t := range runes {
		if i > 0 {
			result.WriteString(", ")
		}
		result.WriteString(runeString(t))
	}
	return result.String()
}

const (
	stTokenStart      = "stTokenStart"
	stQuotedString    = "stQuotedString"
	stEscape          = "stEscape"
	stNumberOrTag     = "stNumberOrTag"
	stIdentifier      = "stIdentifier"
	stTagName         = "stTagName"
	stNumber          = "stNumber"
	stPrefixNumber    = "stPrefixNumber"
	stTrailingSlash   = "stTrailingSlash"
	stTrailingComment = "stTrailingComment"
)

func parseTokens(input string) iter.Seq[token] {
	return func(yield func(token) bool) {
		var buf bytes.Buffer
		var incomplete bool
		var quote rune
		var r rune
		var i int
		var tokenStart int
		runes := []rune(input)

		yieldToken := func(r rune) {
			s := buf.String()
			buf.Reset()
			yield(token{r: r, pos: tokenStart, value: s})
		}
		yieldError := func(err error) {
			buf.Reset()
			yield(token{r: ERROR, pos: tokenStart, value: err.Error()})
		}

		st := stTokenStart
	parseLoop:
		for i = 0; i < len(runes); i++ {
			r = runes[i]
			switch st {
			case stTokenStart:
				tokenStart = i
				switch {
				case unicode.IsSpace(r):
					continue
				case r == '"' || r == '`':
					incomplete = true
					quote = r
					st = stQuotedString
				case r == '+':
					incomplete = true
					st = stNumberOrTag
				case r == '0':
					buf.WriteRune(r)
					st = stPrefixNumber
				case r == '-' || unicode.IsDigit(r):
					buf.WriteRune(r)
					incomplete = true
					st = stNumber
				case isIdentBegin(r):
					buf.WriteRune(r)
					st = stIdentifier
				case r == '/':
					incomplete = true
					st = stTrailingSlash
				case r == '(' || r == ')' || r == '=' || r == ',' || r == ':':
					yieldToken(r)
				default:
					break parseLoop
				}
			case stQuotedString:
				switch {
				case r == '\\':
					st = stEscape
				case r == quote:
					incomplete = false
					yieldToken(STRING)
					st = stTokenStart
				default:
					buf.WriteRune(r)
				}
			case stEscape:
				switch {
				case r == quote || r == '\\':
					buf.WriteRune(r)
					st = stQuotedString
				default:
					yieldError(fmt.Errorf("unhandled escaped character %q", r))
				}
			case stNumberOrTag:
				switch {
				case isIdentBegin(r):
					buf.WriteRune(r)
					st = stTagName
				case r == '0':
					buf.WriteRune(r)
					st = stPrefixNumber
				case r == '-' || unicode.IsDigit(r):
					buf.WriteRune(r)
					st = stNumber
				default:
					break parseLoop
				}
			case stPrefixNumber:
				switch {
				case unicode.IsDigit(r):
					buf.WriteRune(r)
					st = stNumber
				case r == 'x' || r == 'o' || r == 'b':
					incomplete = true
					buf.WriteRune(r)
					st = stNumber
				default:
					incomplete = false
					st = stTokenStart
					yieldToken(NUMBER)
				}
			case stNumber:
				hexits := "abcdefABCDEF"
				switch {
				case unicode.IsDigit(r) || strings.Contains(hexits, string(r)):
					buf.WriteRune(r)
					incomplete = false
				default:
					incomplete = false
					st = stTokenStart
					yieldToken(NUMBER)

					st = stTokenStart
					i--
				}
			case stIdentifier:
				switch {
				case isIdentInterior(r):
					buf.WriteRune(r)
				default:
					s := buf.String()
					if s == "true" || s == "false" {
						yieldToken(BOOLEAN)
					} else {
						yieldToken(IDENTIFIER)
					}
					st = stTokenStart
					i--
				}
			case stTagName:
				switch {
				case isTagNameInterior(r):
					buf.WriteRune(r)
				default:
					incomplete = false
					yieldToken(TAG_NAME)

					st = stTokenStart
					i--
				}
			case stTrailingSlash:
				switch {
				case r == '/':
					incomplete = false
					st = stTrailingComment
				default:
					break parseLoop
				}
			case stTrailingComment:
				i = len(runes)
				break parseLoop
			default:
				yieldError(fmt.Errorf("unexpected internal parser yieldError: unknown state: %s at position %d", st, i))
			}
		}
		switch st {
		case stIdentifier:
			s := buf.String()
			if s == "true" || s == "false" {
				yieldToken(BOOLEAN)
			} else {
				yieldToken(IDENTIFIER)
			}
		case stNumber:
			incomplete = false
			yieldToken(NUMBER)
		case stTagName:
			incomplete = false
			yieldToken(TAG_NAME)
		}
		if i < len(runes) {
			yieldError(fmt.Errorf("unexpected character %q at position %d", runes[i], i))
		}
		if incomplete {
			yieldError(fmt.Errorf("unexpected end of input, lexer state: %s", st))
		}
	}
}

const (
	EOF        = -1
	NUMBER     = -2
	STRING     = -3
	BOOLEAN    = -4
	IDENTIFIER = -5
	TAG_NAME   = -6
	ERROR      = -7
)

type lexer struct {
	iterNext func() (token, bool)
	iterStop func()
	endIdx   int
	cur      token
	nxt      token
}

func newLexer(input string) *lexer {
	iterNext, iterStop := iter.Pull(parseTokens(input))
	l := &lexer{
		iterNext: iterNext,
		iterStop: iterStop,
		endIdx:   len(input),
	}
	l.cur = l.pull()
	l.nxt = l.pull()
	return l
}

func (l *lexer) pull() token {
	v, ok := l.iterNext()
	if !ok {
		return token{r: EOF, pos: l.endIdx}
	}
	return v
}

func (l *lexer) next() token {
	v := l.cur
	l.cur = l.nxt
	l.nxt = l.pull()
	return v
}

func (l *lexer) peek() token {
	return l.cur
}

func (l *lexer) peek2() token {
	return l.nxt
}
