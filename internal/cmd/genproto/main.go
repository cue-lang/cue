// Copyright 2026 CUE Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// genproto generates the Go code for the proto files under
// encoding/protobuf without needing protoc, so that a plain
// "go generate ./..." run keeps the generated code up to date.
// It must be run from the encoding/protobuf/cueproto directory,
// which its go:generate directive takes care of.
//
// protocompile replaces protoc for compiling the proto sources into
// descriptors, and protoc-gen-go's generator package is called directly
// rather than as a plugin subprocess, so both are pinned via go.mod.
// The output mimics protoc v35.1 byte for byte, including its version
// marker in the generated headers.
package main

import (
	"context"
	"log"
	"os"
	"path/filepath"

	"github.com/bufbuild/protocompile"
	"github.com/bufbuild/protocompile/protoutil"
	gengo "google.golang.org/protobuf/cmd/protoc-gen-go/internal_gengo"
	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/pluginpb"
)

// The proto include root and the files to generate, relative to the
// working directory.
const root = ".."

var protoFiles = []string{"cueproto/cue.proto", "cue/cue.proto"}

// protocVersion is the protoc release whose behavior the generated
// output mimics, recorded in the generated headers. protoc uses major
// version 7 for its v35.x releases.
var protocVersion = &pluginpb.Version{
	Major: proto.Int32(7), Minor: proto.Int32(35), Patch: proto.Int32(1),
}

func main() {
	log.SetFlags(0)
	comp := protocompile.Compiler{
		Resolver: protocompile.WithStandardImports(&protocompile.SourceResolver{
			ImportPaths: []string{root},
		}),
		SourceInfoMode: protocompile.SourceInfoStandard,
	}
	files, err := comp.Compile(context.Background(), protoFiles...)
	if err != nil {
		log.Fatal(err)
	}

	// The request wants all files including transitive dependencies,
	// in topological order.
	var fdProtos []*descriptorpb.FileDescriptorProto
	seen := make(map[string]bool)
	var add func(fd protoreflect.FileDescriptor)
	add = func(fd protoreflect.FileDescriptor) {
		if seen[fd.Path()] {
			return
		}
		seen[fd.Path()] = true
		imports := fd.Imports()
		for i := range imports.Len() {
			add(imports.Get(i).FileDescriptor)
		}
		fdProtos = append(fdProtos, protoutil.ProtoFromFileDescriptor(fd))
	}
	for _, fd := range files {
		add(fd)
	}

	req := &pluginpb.CodeGeneratorRequest{
		FileToGenerate:  protoFiles,
		Parameter:       proto.String("paths=source_relative"),
		ProtoFile:       fdProtos,
		CompilerVersion: protocVersion,
	}
	gen, err := protogen.Options{}.New(req)
	if err != nil {
		log.Fatal(err)
	}
	gen.SupportedFeatures = gengo.SupportedFeatures
	gen.SupportedEditionsMinimum = gengo.SupportedEditionsMinimum
	gen.SupportedEditionsMaximum = gengo.SupportedEditionsMaximum
	for _, f := range gen.Files {
		if f.Generate {
			gengo.GenerateFile(gen, f)
		}
	}
	resp := gen.Response()
	if resp.Error != nil {
		log.Fatal(*resp.Error)
	}
	for _, f := range resp.File {
		name := filepath.Join(root, filepath.FromSlash(f.GetName()))
		if err := os.WriteFile(name, []byte(f.GetContent()), 0o666); err != nil {
			log.Fatal(err)
		}
	}
}
