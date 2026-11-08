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

package cueproto_test

import (
	"testing"

	"github.com/go-quicktest/qt"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"

	"cuelang.org/go/encoding/protobuf/cueproto"
)

func TestExtensions(t *testing.T) {
	opts := &descriptorpb.FieldOptions{}
	proto.SetExtension(opts, cueproto.E_Val, `=~"^[A-Z]"`)
	proto.SetExtension(opts, cueproto.E_Opt, &cueproto.FieldOptions{Required: true})

	data, err := proto.Marshal(opts)
	qt.Assert(t, qt.IsNil(err))

	got := &descriptorpb.FieldOptions{}
	err = proto.Unmarshal(data, got)
	qt.Assert(t, qt.IsNil(err))

	qt.Assert(t, qt.Equals(proto.GetExtension(got, cueproto.E_Val).(string), `=~"^[A-Z]"`))
	qt.Assert(t, qt.IsTrue(proto.GetExtension(got, cueproto.E_Opt).(*cueproto.FieldOptions).GetRequired()))
}
