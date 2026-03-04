package gomailconn

import (
	"testing"
)

func TestSanitizeHeaderValue(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", ""},
		{"no CR/LF", "hello world", "hello world"},
		{"removes CR", "hello\rworld", "helloworld"},
		{"removes LF", "hello\nworld", "helloworld"},
		{"removes CRLF", "hello\r\nworld", "helloworld"},
		{"multiple", "a\rb\nc\r\nd", "abcd"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sanitizeHeaderValue(tt.in)
			if got != tt.want {
				t.Errorf("sanitizeHeaderValue(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
