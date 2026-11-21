package transport

import (
	"os/exec"
	"testing"
)

func TestRemoteWordRoundTrip(t *testing.T) {
	for _, word := range []string{"", "list", "-v", "guest one", "it's a VM", "a\nb", "$HOME", "a;b", "*"} {
		// A POSIX shell on the receiving side must see exactly one original argument.
		got, err := exec.Command("sh", "-c", "set -- "+shellWord(word)+`; test "$#" -eq 1 && printf '%s' "$1"`).Output()
		if err != nil || string(got) != word {
			t.Errorf("word %q: got %q, err %v", word, got, err)
		}
	}
}
