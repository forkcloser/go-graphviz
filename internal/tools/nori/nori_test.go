package nori_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/goccy/nori"
	"google.golang.org/protobuf/types/pluginpb"
)

func TestNori(t *testing.T) {
	bindProto := filepath.Join("internal", "wasm", "bind.proto")
	ctx := context.Background()
	files, err := nori.ProtoCompile(ctx, bindProto, filepath.Join(".."), filepath.Join("proto"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := nori.Generate(ctx, &pluginpb.CodeGeneratorRequest{
		ProtoFile: files,
	}); err != nil {
		t.Fatal(err)
	}
}
