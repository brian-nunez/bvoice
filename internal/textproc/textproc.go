// Package textproc cleans Whisper output and builds decoder prompts. Pure Go, no platform code.
package textproc

import (
	"regexp"
	"strings"
)

// nonSpeech matches Whisper's non-speech annotations such as [BLANK_AUDIO], [Music], (laughs), *coughs*.
var nonSpeech = regexp.MustCompile(`\[[^\]]*\]|\([^)]*\)|\*[^*]*\*`)

// Clean removes non-speech markers and normalises whitespace.
func Clean(raw string) string {
	return strings.Join(strings.Fields(nonSpeech.ReplaceAllString(raw, " ")), " ")
}

// Join concatenates cleaned chunk texts into one transcript.
func Join(parts []string) string {
	var out []string

	for _, p := range parts {
		if p = Clean(p); p != "" {
			out = append(out, p)
		}
	}

	return strings.Join(out, " ")
}

// Tail returns roughly the last n characters of s, starting at a word boundary.
func Tail(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}

	cut := s[len(s)-n:]
	if i := strings.IndexByte(cut, ' '); i >= 0 && i < len(cut)-1 {
		cut = cut[i+1:]
	}

	return cut
}

// Prompt builds the initial prompt for a chunk: the user's custom vocabulary followed by the tail of
// what has been transcribed so far, so names are spelled right and punctuation flows across chunks.
func Prompt(vocabulary, previous string) string {
	var parts []string
	if v := strings.Join(strings.Fields(vocabulary), " "); v != "" {
		parts = append(parts, v)
	}

	if t := Tail(previous, 200); t != "" {
		parts = append(parts, t)
	}

	return strings.Join(parts, " ")
}
