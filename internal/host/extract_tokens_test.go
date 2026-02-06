package host

import (
	"reflect"
	"testing"
)

func TestExtractTokens(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []string
	}{
		{
			name:     "Normal hex tokens",
			input:    "41 42 43",
			expected: []string{"41", "42", "43"},
		},
		{
			name:     "Mixed SF and SA tokens",
			input:    "SF(c0=c0) SA(41=f1) 41",
			expected: []string{"SF(c0=c0)", "41"},
		},
		{
			name:     "Multiple spaces and tabs",
			input:    "41  42\t43",
			expected: []string{"41", "42", "43"},
		},
		{
			name:     "Leading and trailing whitespace",
			input:    "  41 42  ",
			expected: []string{"41", "42"},
		},
		{
			name:     "Invalid tokens ignored",
			input:    "FOO BAR 1 123 XX",
			expected: []string{},
		},
		{
			name:     "Empty string",
			input:    "",
			expected: []string{},
		},
		{
			name:     "Only invalid tokens",
			input:    "SA(1) NOTHEX 123",
			expected: []string{},
		},
		{
			name:     "Valid hex variations",
			input:    "00 FF ab AB 1a 9F",
			expected: []string{"00", "FF", "ab", "AB", "1a", "9F"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractTokens(tt.input)
			if len(got) == 0 && len(tt.expected) == 0 {
				return
			}
			if !reflect.DeepEqual(got, tt.expected) {
				t.Errorf("extractTokens(%q) = %v, want %v", tt.input, got, tt.expected)
			}
		})
	}
}
