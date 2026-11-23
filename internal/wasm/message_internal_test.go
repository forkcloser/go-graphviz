package wasm

import (
	"context"
	"testing"
)

// A stay in the module begins with no error to read: what Graphviz reported
// to the calls before it, and none of them read, is dropped, so that it is
// neither taken for the stay's own nor kept for good. A stay inside
// another's begins where that one stands, takes what is reported after,
// and leaves the rest to it.
func TestAStayBeginsWithNothingReported(t *testing.T) {
	// As Graphviz's agerrf sends an error: the level, the separator, the text.
	report := func(text string) {
		t.Helper()

		for _, fragment := range []string{levelError, levelSeparator, text} {
			if _, err := onMessage(t.Context(), fragment); err != nil {
				t.Fatal(err)
			}
		}
	}

	report("left by an earlier call")

	err := Exclusive(t.Context(), func(ctx context.Context) error {
		if mark := ErrorMark(); mark != 0 {
			t.Errorf("a stay begins at %d bytes of errors, want none", mark)
		}

		report("the outer call's")

		outer := ErrorMark()

		err := Exclusive(ctx, func(context.Context) error {
			mark := ErrorMark()
			if mark != outer {
				t.Errorf("a stay inside another's begins at %d, want where that one stands, %d", mark, outer)
			}

			report("the inner call's")

			if got := TakeErrorsSince(mark); got != "\nthe inner call's" {
				t.Errorf("the inner call read %q, want its own error", got)
			}

			return nil
		})

		if got := TakeErrorsSince(0); got != "the outer call's" {
			t.Errorf("the outer call read %q, want its own error", got)
		}

		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}
