package main

import (
	"context"
	"io"
	"log/slog"
	"os"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/pluginpb"

	"github.com/forkcloser/go-graphviz/internal/tools/nori"
)

func main() {
	if err := _main(); err != nil {
		// protoc reads the response from stdout; the error goes to stderr.
		slog.Error("protoc-gen-nori", "err", err)
		os.Exit(1)
	}
}

func _main() error {
	req, err := parseRequest(os.Stdin)
	if err != nil {
		return err
	}
	resp, err := nori.Generate(context.Background(), req)
	if err != nil {
		return err
	}
	if resp == nil {
		return nil
	}
	if err := outputResponse(resp); err != nil {
		return err
	}
	return nil
}

func parseRequest(r io.Reader) (*pluginpb.CodeGeneratorRequest, error) {
	buf, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}

	var req pluginpb.CodeGeneratorRequest
	if err := proto.Unmarshal(buf, &req); err != nil {
		return nil, err
	}
	return &req, nil
}

func outputResponse(resp *pluginpb.CodeGeneratorResponse) error {
	buf, err := proto.Marshal(resp)
	if err != nil {
		return err
	}
	if _, err = os.Stdout.Write(buf); err != nil {
		return err
	}
	return nil
}
