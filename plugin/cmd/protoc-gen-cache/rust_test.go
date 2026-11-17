package main

import (
	"testing"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/pluginpb"
)

// A lang=rust request needs no go_package: each file without one is mapped to
// a placeholder, and a file that has one, or an M mapping, is left alone.
func TestRustRequestNeedsNoGoPackage(t *testing.T) {
	req := &pluginpb.CodeGeneratorRequest{
		Parameter: proto.String("lang=rust,Mmapped.proto=example.com/m"),
		ProtoFile: []*descriptorpb.FileDescriptorProto{
			{Name: proto.String("a/v1/a.proto")},
			{Name: proto.String("b.proto"), Options: &descriptorpb.FileOptions{GoPackage: proto.String("example.com/b")}},
			{Name: proto.String("mapped.proto")},
		},
	}
	if !rustRequest(req.GetParameter()) || rustRequest("lang=go") {
		t.Fatal("rustRequest misreads lang")
	}
	got := withPlaceholderImports(req)
	want := "lang=rust,Mmapped.proto=example.com/m,Ma/v1/a.proto=protoc-gen-cache.invalid/rust/a/v1"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
