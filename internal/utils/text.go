package utils

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

var ansiPattern = regexp.MustCompile(`\x1b\[[0-9;]*[A-Za-z]`)

func StripANSI(text string) string {
	return ansiPattern.ReplaceAllString(text, "")
}

func DisplayWidth(text string) int {
	return utf8.RuneCountInString(StripANSI(text))
}

func TruncateMiddle(text string, maxRunes int) string {
	if maxRunes <= 0 {
		return text
	}
	runes := []rune(text)
	if len(runes) <= maxRunes {
		return text
	}
	keepHead := maxRunes / 2
	keepTail := maxRunes - keepHead
	if keepHead < 1 {
		keepHead = 1
	}
	if keepTail < 1 {
		keepTail = 1
	}
	return string(runes[:keepHead]) + "\n... [truncated] ...\n" + string(runes[len(runes)-keepTail:])
}

func NormalizeWhitespace(text string) string {
	fields := strings.Fields(text)
	return strings.Join(fields, " ")
}
