package proc

import (
	"context"
	"errors"
	"testing"
)

func TestStreamMissingBinary(t *testing.T) {
	err := Stream(context.Background(), "", "nfu-definitely-not-a-real-binary")
	if err == nil {
		t.Fatal("missing binary should error")
	}
}

func TestStreamExit(t *testing.T) {
	ctx := context.Background()
	// `true` exits 0; `false` exits non-zero → a CmdError.
	if err := Stream(ctx, "", "true"); err != nil {
		t.Errorf("true: %v", err)
	}
	err := Stream(ctx, "", "false")
	if err == nil {
		t.Fatal("false should exit non-zero")
	}
	var ce *CmdError
	if !errors.As(err, &ce) || ce.Name != "false" {
		t.Errorf("want CmdError for false, got %v", err)
	}
}
