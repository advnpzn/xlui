// Package terminal renders external text for a terminal display.
package terminal

import (
	"io"
	"os"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/term"
	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
)

func displayRune(r rune) rune {
	if unicode.IsControl(r) || unicode.In(r, unicode.Cf, unicode.Zl, unicode.Zp) {
		return utf8.RuneError
	}
	return r
}

// SingleLine keeps external text inside its assigned cell or label.
func SingleLine(s string) string { return strings.Map(displayRune, s) }

// Output preserves raw bytes for pipelines and files. On a terminal it keeps
// tabs and line feeds for layout and buffers incomplete UTF-8 across writes.
// Call finish to flush a final incomplete character. The caller owns the file.
func Output(f *os.File) (w io.Writer, finish func() error) {
	if !term.IsTerminal(int(f.Fd())) {
		return f, func() error { return nil }
	}
	stream := displayStream(f)
	return stream, stream.Close
}

func displayStream(w io.Writer) *transform.Writer {
	return transform.NewWriter(w, runes.Map(func(r rune) rune {
		if r == '\t' || r == '\n' {
			return r
		}
		return displayRune(r)
	}))
}
