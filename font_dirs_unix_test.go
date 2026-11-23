//go:build unix

package graphviz_test

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// What is named like a font and is no file is not opened: a named pipe in a
// font directory, which nothing writes to, kept the first label of the
// process waiting for ever.
func TestAPipeNamedLikeAFontIsNotRead(t *testing.T) {
	if os.Getenv(fontCorpusEnv) != "" {
		if got := chosenAdvance(t, "Zp Beside"); got != 200 {
			t.Errorf("the font beside the pipe is not found: an A %.0f units wide, want 200", got)
		}

		return
	}

	home := t.TempDir()

	dir := userFontDirectory(home)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}

	if err := syscall.Mkfifo(filepath.Join(dir, "pipe.ttf"), 0o600); err != nil {
		t.Skipf("no named pipe here: %v", err)
	}

	face := corpusFace{family: "Zp Beside", subfamily: "Regular", advance: 200}
	if err := os.WriteFile(filepath.Join(dir, "beside.ttf"), face.font(t), 0o600); err != nil {
		t.Fatal(err)
	}

	// The child is killed at its deadline, a minute: a process that waits on
	// the pipe fails the test there.
	cmd := child(t, os.Args[0], "-test.run=^"+t.Name()+"$", "-test.v")

	cmd.Env = append(os.Environ(), fontCorpusEnv+"=1", "HOME="+home)

	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("the process with a pipe in its font directory: %v\n%s", err, out)
	}
}
