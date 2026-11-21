package transport

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestRunnerBoundsInheritedOutputPipe(t *testing.T) {
	dir := t.TempDir()
	release := filepath.Join(dir, "release")
	// The shell exits immediately. Its child holds stdout until we release it.
	script := `(while [ -d "$1" ] && [ ! -f "$1/release" ]; do sleep 0.05; done) &`
	var out bytes.Buffer
	done := make(chan error, 1)
	go func() {
		done <- (Runner{}).Run(context.Background(), &out, nil, "sh", "-c", script, "sh", dir)
	}()
	select {
	case err := <-done:
		if !errors.Is(err, exec.ErrWaitDelay) {
			t.Errorf("got %v, want the inherited pipe to time out", err)
		}
	case <-time.After(5 * time.Second):
		t.Error("runner still waiting on inherited stdout")
	}
	if err := os.WriteFile(release, nil, 0600); err != nil {
		t.Fatal(err)
	}
}

func TestRemoteWordRoundTrip(t *testing.T) {
	for _, word := range []string{"", "list", "-v", "guest one", "it's a VM", "a\nb", "$HOME", "a;b", "*"} {
		// A POSIX shell on the receiving side must see exactly one original argument.
		got, err := exec.Command("sh", "-c", "set -- "+shellWord(word)+`; test "$#" -eq 1 && printf '%s' "$1"`).Output()
		if err != nil || string(got) != word {
			t.Errorf("word %q: got %q, err %v", word, got, err)
		}
	}
}
