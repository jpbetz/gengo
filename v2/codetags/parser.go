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
	"fmt"
	"slices"
	"strings"
	"unicode"
)

// Parse parses a tag string into a TypedTag, or returns an error if the tag
// string fails to parse.
//
// ParseOption may be provided to modify the behavior of the parser. The below
// describes the default behavior.
//
// A tag consists of a name, optional arguments, and an optional scalar value or
// tag value. For example,
//
//	"name"
//	"name=50"
//	"name("featureX")=50"
//	"name(limit: 10, path: "/xyz")=text value"
//	"name(limit: 10, path: "/xyz")=+anotherTag(size: 100)"
//
// Arguments are optional and may be either:
//   - A single positional argument.
//   - One or more named arguments (in the format `name: value`).
//   - (Positional and named arguments cannot be mixed.)
//
// For example,
//
//	"name()"
//	"name(arg)"
//	"name(namedArg1: argValue1)"
//	"name(namedArg1: argValue1, namedArg2: argValue2)"
//
// Argument values may be strings, ints, booleans, or identifiers.
//
// For example,
//
//	"name("double-quoted")"
//	"name(`backtick-quoted`)"
//	"name(100)"
//	"name(true)"
//	"name(arg1: identifier)"
//	"name(arg1:`string value`)"
//	"name(arg1: 100)"
//	"name(arg1: true)"
//
// Note: When processing Go source code comments, the Extract function is
// typically used first to find and isolate tag strings matching a specific
// prefix. Those extracted strings can then be parsed using this function.
//
// The value part of the tag is optional and follows an equals sign "=". If a
// value is present, it must be a string, int, boolean, identifier, or tag.
//
// For example,
//
//	"name" // no value
//	"name=identifier"
//	"name="double-quoted value""
//	"name=`backtick-quoted value`"
//	"name(100)"
//	"name(true)"
//	"name=+anotherTag"
//	"name=+anotherTag(size: 100)"
//
// Trailing comments are ignored unless the RawValues option is enabled, in which
// case they are treated as part of the value.
//
// For example,
//
//	"key=value // This comment is ignored"
//
// Formal Grammar:
//
// <tag>             ::= <tagName> [ "(" [ <args> ] ")" ] [ ( "=" <value> | "=+" <tag> ) ]
// <args>            ::= <value> | <namedArgs>
// <namedArgs>       ::= <argNameAndValue> [ "," <namedArgs> ]*
// <argNameAndValue> ::= <identifier> ":" <value>
// <value>           ::= <identifier> | <string> | <int> | <bool>
//
// <tagName>       ::= [a-zA-Z_][a-zA-Z0-9_-.:]*
// <identifier>    ::= [a-zA-Z_][a-zA-Z0-9_-.]*
// <string>        ::= /* Go-style double-quoted or backtick-quoted strings,
// ...                    with standard Go escape sequences for double-quoted strings. */
// <int>           ::= /* Standard Go integer literals (decimal, 0x hex, 0o octal, 0b binary),
// ...                    with an optional +/- prefix. */
// <bool>          ::= "true" | "false"
func Parse(tag string, options ...ParseOption) (TypedTag, error) {
	opts := parseOpts{}
	for _, o := range options {
		o(&opts)
	}

	tag = strings.TrimSpace(tag)
	return parseTag(tag, opts)
}

// ParseAll calls Parse on each tag in the input slice.
func ParseAll(tags []string, options ...ParseOption) ([]TypedTag, error) {
	var out []TypedTag
	for _, tag := range tags {
		parsed, err := Parse(tag, options...)
		if err != nil {
			return nil, err
		}
		out = append(out, parsed)
	}
	return out, nil
}

type parseOpts struct {
	rawValues bool
}

// ParseOption provides a parser option.
type ParseOption func(*parseOpts)

// RawValues skips parsing of the value part of the tag. If enabled, the Value
// in the parse response will contain all text following the "=" sign, up to the last
// non-whitespace character, and ValueType will be set to ValueTypeRaw.
// Default: disabled
func RawValues(enabled bool) ParseOption {
	return func(opts *parseOpts) {
		opts.rawValues = enabled
	}
}

const (
	stTag        = "stTag"
	stMaybeArgs  = "stMaybeArgs"
	stArg        = "stArg"
	stMaybeValue = "stMaybeValue"
	stValue      = "stValue"
)

func parseTag(input string, opts parseOpts) (TypedTag, error) {
	var startTag, endTag *TypedTag

	// These are defined outside the loop to make errors easier.
	s := newLexer("+" + input)
	var incomplete bool

	st := stTag
parseLoop:
	for s.peek().r != EOF {
		switch st {
		case stTag:
			switch {
			case s.peek().r == TAG_NAME:
				tagName := s.next().value
				newTag := &TypedTag{Name: tagName}
				if startTag == nil {
					startTag = newTag
					endTag = startTag
				} else {
					endTag.ValueTag = newTag
					endTag.ValueType = ValueTypeTag
					endTag = newTag
				}
				st = stMaybeArgs
			default:
				break parseLoop
			}
		case stMaybeArgs:
			switch {
			case s.peek().r == '(':
				s.next()
				incomplete = true
				st = stArg
			case s.peek().r == '=':
				if opts.rawValues {
					endTag.ValueType = ValueTypeRaw
				}
				s.next()
				st = stValue
			default:
				break parseLoop
			}
		case stArg:
			if endTag == nil {
				return TypedTag{}, fmt.Errorf("unexpected parser state: expected tag to exist")
			}
			switch {
			case s.peek().r == ')': // 0 args
				s.next()
				incomplete = false
				st = stMaybeValue
			case s.peek2().r == ')': // 1 positional arg
				value := s.next()
				valueType := toArgType(value.r)
				if valueType == "unknown" {
					return TypedTag{}, fmt.Errorf("unknown argument value type %q", value.value)
				}
				arg := Arg{Value: value.value, Type: valueType}
				endTag.Args = append(endTag.Args, arg)

				s.next() // consume )
				incomplete = false
				st = stMaybeValue
			case s.peek().r == IDENTIFIER: // named args
				key := s.next()
				if s.next().r != ':' {
					return TypedTag{}, fmt.Errorf("expected ':' after argument name %q", key.value)
				}
				value := s.next()
				valueType := toArgType(value.r)
				if valueType == "unknown" {
					return TypedTag{}, fmt.Errorf("unknown argument value type %q", value.value)
				}
				arg := Arg{Name: key.value, Value: value.value, Type: valueType}
				endTag.Args = append(endTag.Args, arg)

				switch {
				case s.peek().r == ',':
					s.next()
					st = stArg
				case s.peek().r == ')':
					s.next()
					incomplete = false
					st = stMaybeValue
				default:
					break parseLoop
				}
			default:
				break parseLoop
			}
		case stMaybeValue:
			switch {
			case s.peek().r == '=':
				if opts.rawValues {
					endTag.ValueType = ValueTypeRaw
				}
				s.next()
				st = stValue
			default:
				break parseLoop
			}
		case stValue:
			switch {
			case opts.rawValues: // When enabled, consume all remaining chars
				endTag.Value = input[s.peek().pos-1:]
				return *startTag, nil
			case s.peek().r == TAG_NAME: // tag value
				st = stTag
			case slices.Contains([]rune{IDENTIFIER, STRING, NUMBER, BOOLEAN}, s.peek().r):
				t := s.next()
				endTag.Value = t.value
				endTag.ValueType = toValueType(t.r)
				st = stTag
			default:
				break parseLoop
			}
		default:
			return TypedTag{}, fmt.Errorf("unexpected internal parser error: unknown state: %s at position %d", st, s.peek().pos)
		}
	}
	if s.peek().r != EOF {
		return TypedTag{}, fmt.Errorf("unexpected token %s(%s) at position %d", runeString(s.peek().r), s.peek().value, s.peek().pos)
	}
	if incomplete {
		return TypedTag{}, fmt.Errorf("unexpected end of input")
	}
	return *startTag, nil
}

func isIdentBegin(r rune) bool {
	return unicode.IsLetter(r) || r == '_'
}

func isIdentInterior(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '.' || r == '-'
}

func isTagNameInterior(r rune) bool {
	return isIdentInterior(r) || r == ':'
}
