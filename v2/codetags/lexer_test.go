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
	"testing"
)

func TestLexer(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []rune
	}{
		{
			name:     "empty input",
			input:    "",
			expected: []rune{EOF},
		},
		{
			name:     "simple runes",
			input:    "( ) = , :",
			expected: []rune{'(', ')', '=', ',', ':', EOF},
		},
		{
			name:     "identifiers",
			input:    "abc def_123",
			expected: []rune{IDENTIFIER, IDENTIFIER, EOF},
		},
		{
			name:     "numbers",
			input:    "123 -456 0x7F",
			expected: []rune{NUMBER, NUMBER, NUMBER, EOF},
		},
		{
			name:     "strings",
			input:    `"hello" "world"`,
			expected: []rune{STRING, STRING, EOF},
		},
		{
			name:     "booleans",
			input:    "true false",
			expected: []rune{BOOLEAN, BOOLEAN, EOF},
		},
		{
			name:     "tag names",
			input:    "+tag1 +tag2:subtag",
			expected: []rune{TAG_NAME, TAG_NAME, EOF},
		},
		{
			name:     "mixed tokens",
			input:    `identifier 123 "string" true +tag (=,)`,
			expected: []rune{IDENTIFIER, NUMBER, STRING, BOOLEAN, TAG_NAME, '(', '=', ',', ')', EOF},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var tokens []rune

			// Create a lexer and tokenize the input
			l := newLexer(tt.input)

			// Collect the token types
			for l.peek().r != EOF {
				tok := l.next()
				if tok.r == ERROR {
					t.Errorf("token error: %v at %d", tok.value, tok.pos)
				}
				tokens = append(tokens, tok.r)
			}
			tokens = append(tokens, EOF)

			if len(tokens) != len(tt.expected) {
				t.Fatalf("expected %d tokens, got %d: %s", len(tt.expected), len(tokens), runesString(tokens))
			}

			for i, expected := range tt.expected {
				actual := tokens[i]
				if actual != expected {
					t.Errorf("token[%d]: expected %s, got %s", i, runeString(expected), runeString(actual))
				}
			}
		})
	}
}
