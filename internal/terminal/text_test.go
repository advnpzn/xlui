package terminal

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestDisplayStream(t *testing.T) {
	const input = "node 雪\tα\nname\r\x1b\a\u009b\u202e\u2028\u2029\xff\xe2"
	const want = "node 雪\tα\nname���������"
	for split := 0; split <= len(input); split++ {
		var out bytes.Buffer
		stream := displayStream(&out)
		for _, part := range []string{input[:split], input[split:]} {
			if _, err := stream.Write([]byte(part)); err != nil {
				t.Fatal(err)
			}
		}
		if err := stream.Close(); err != nil {
			t.Fatal(err)
		}
		if got := out.String(); got != want {
			t.Fatalf("split at %d: got %q, want %q", split, got, want)
		}
	}
	if got := SingleLine("node 雪\tα\n\u2028"); got != "node 雪�α��" {
		t.Fatalf("single-line display: %q", got)
	}
}

func TestRedirectPreservesBytes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "inventory")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	out, finish := Output(f)
	want := []byte("node\r\n\t\x1b\xff")
	if _, err := out.Write(want); err != nil {
		t.Fatal(err)
	}
	if err := finish(); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("redirected bytes %q, err %v", got, err)
	}
}
