package graphviz_test

import (
	"context"
	"os/exec"
	"testing"
	"time"
)

// childDeadline is what a process the tests start may take: a test binary
// killed at its own timeout does not kill its children.
const childDeadline = time.Minute

// child is a command that is killed at its deadline, or when its test ends.
func child(t *testing.T, name string, args ...string) *exec.Cmd {
	t.Helper()

	ctx, cancel := context.WithTimeout(t.Context(), childDeadline)
	t.Cleanup(cancel)

	cmd := exec.CommandContext(ctx, name, args...)
	cmd.WaitDelay = time.Second

	return cmd
}
