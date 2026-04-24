package utils

import "testing"

func TestStripANSIAndDisplayWidth(t *testing.T) {
	input := "\x1b[31mred\x1b[0m and \x1b[1;32mgreen\x1b[0m"

	if got := StripANSI(input); got != "red and green" {
		t.Fatalf("StripANSI() = %q, want %q", got, "red and green")
	}

	if got := DisplayWidth(input); got != len("red and green") {
		t.Fatalf("DisplayWidth() = %d, want %d", got, len("red and green"))
	}
}

func TestTruncateMiddle(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		maxRunes int
		want    string
	}{
		{
			name:     "zero keeps input",
			input:    "abcdef",
			maxRunes: 0,
			want:     "abcdef",
		},
		{
			name:     "short input stays intact",
			input:    "abcdef",
			maxRunes: 6,
			want:     "abcdef",
		},
		{
			name:     "truncates middle",
			input:    "abcdefgh",
			maxRunes: 4,
			want:     "ab\n... [truncated] ...\ngh",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := TruncateMiddle(tt.input, tt.maxRunes); got != tt.want {
				t.Fatalf("TruncateMiddle() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestNormalizeWhitespace(t *testing.T) {
	input := "  hello\t\n  brave   new\tworld  "
	want := "hello brave new world"

	if got := NormalizeWhitespace(input); got != want {
		t.Fatalf("NormalizeWhitespace() = %q, want %q", got, want)
	}
}