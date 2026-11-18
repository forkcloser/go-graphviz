package wasm

import (
	"context"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// cacheChild marks the process TestLoadsOverACacheThatCannotBeUsed starts:
// the module loads once, when the package is, from the cache its
// environment names.
const cacheChild = "GO_GRAPHVIZ_CACHE_CHILD"

// The module loads when wazero's compiled-code cache holds an entry that
// can be neither read nor replaced, compiling without the cache. The load
// failed, for good in that process: on Windows a process starting while
// another wrote the same entry was refused the rename onto it, which is how
// two test binaries starting together on an empty cache failed one another.
func TestLoadsOverACacheThatCannotBeUsed(t *testing.T) {
	if os.Getenv(cacheChild) != "" {
		if _, err := mod.malloc(t.Context(), 8); err != nil {
			t.Fatal(err)
		}

		return
	}

	// Where os.UserCacheDir looks, on each platform.
	home := t.TempDir()
	env := append(os.Environ(), cacheChild+"=1", "HOME="+home, "XDG_CACHE_HOME="+home, "LocalAppData="+home)

	start := func() ([]byte, error) {
		ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
		defer cancel()

		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestLoadsOverACacheThatCannotBeUsed$")
		cmd.Env = env
		cmd.WaitDelay = time.Second

		return cmd.CombinedOutput()
	}

	if out, err := start(); err != nil {
		t.Fatalf("a start on an empty cache: %v\n%s", err, out)
	}

	// Each entry the first start left becomes a directory of its name:
	// nothing is read from it, and nothing renamed onto it.
	var entries []string

	err := filepath.WalkDir(home, func(path string, entry fs.DirEntry, err error) error {
		if err == nil && entry.Type().IsRegular() {
			entries = append(entries, path)
		}

		return err
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(entries) == 0 {
		t.Fatal("the first start left no entry in its cache: the test measures nothing")
	}

	for _, entry := range entries {
		if err := os.Remove(entry); err != nil {
			t.Fatal(err)
		}

		if err := os.Mkdir(entry, 0o700); err != nil {
			t.Fatal(err)
		}
	}

	if out, err := start(); err != nil {
		t.Fatalf("a start over a cache that cannot be used: %v\n%s", err, out)
	}
}
