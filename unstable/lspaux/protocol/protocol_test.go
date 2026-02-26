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

package protocol_test

import (
	"bytes"
	"testing"

	"cuelang.org/go/unstable/lspaux/protocol"
)

func TestChangedMsgRoundTrip(t *testing.T) {
	orig := &protocol.ChangedMsg{}
	data := orig.MarshalBytes()

	if len(data) != 1 || data[0] != protocol.MsgTypeChanged {
		t.Fatalf("MarshalBytes: got %x, want [01]", data)
	}

	var decoded protocol.ChangedMsg
	if err := decoded.UnmarshalBytes(data); err != nil {
		t.Fatalf("UnmarshalBytes: %v", err)
	}
}

func TestChangedMsgRejectsTrailingBytes(t *testing.T) {
	var m protocol.ChangedMsg
	if err := m.UnmarshalBytes([]byte{0x01, 0x00}); err == nil {
		t.Fatal("expected error for trailing bytes")
	}
}

func TestChangedMsgRejectsWrongType(t *testing.T) {
	var m protocol.ChangedMsg
	if err := m.UnmarshalBytes([]byte{0x02}); err == nil {
		t.Fatal("expected error for wrong type byte")
	}
}

func TestChangedMsgRejectsEmpty(t *testing.T) {
	var m protocol.ChangedMsg
	if err := m.UnmarshalBytes(nil); err == nil {
		t.Fatal("expected error for empty input")
	}
}

func TestEvalRequestMsgRoundTrip(t *testing.T) {
	orig := &protocol.EvalRequestMsg{
		RequestID: "req-42",
		RepoName:  "https://cue.gerrithub.io/a/cue-lang/cue",
		CommitID:  "abc123def456",
		ZipData:   []byte{0x50, 0x4B, 0x03, 0x04, 0xDE, 0xAD},
	}

	data := orig.MarshalBytes()

	var decoded protocol.EvalRequestMsg
	if err := decoded.UnmarshalBytes(data); err != nil {
		t.Fatalf("UnmarshalBytes: %v", err)
	}

	if decoded.RequestID != orig.RequestID {
		t.Errorf("RequestID: got %q, want %q", decoded.RequestID, orig.RequestID)
	}
	if decoded.RepoName != orig.RepoName {
		t.Errorf("RepoName: got %q, want %q", decoded.RepoName, orig.RepoName)
	}
	if decoded.CommitID != orig.CommitID {
		t.Errorf("CommitID: got %q, want %q", decoded.CommitID, orig.CommitID)
	}
	if !bytes.Equal(decoded.ZipData, orig.ZipData) {
		t.Errorf("ZipData: got %x, want %x", decoded.ZipData, orig.ZipData)
	}
}

func TestEvalRequestMsgEmptyZip(t *testing.T) {
	orig := &protocol.EvalRequestMsg{
		RequestID: "r1",
		RepoName:  "repo",
		CommitID:  "aaa",
		ZipData:   nil,
	}

	data := orig.MarshalBytes()

	var decoded protocol.EvalRequestMsg
	if err := decoded.UnmarshalBytes(data); err != nil {
		t.Fatalf("UnmarshalBytes: %v", err)
	}

	if len(decoded.ZipData) != 0 {
		t.Errorf("ZipData: got length %d, want 0", len(decoded.ZipData))
	}
}

func TestEvalRequestMsgRejectsWrongType(t *testing.T) {
	var m protocol.EvalRequestMsg
	if err := m.UnmarshalBytes([]byte{0x01}); err == nil {
		t.Fatal("expected error for wrong type byte")
	}
}

func TestEvalRequestMsgRejectsTruncated(t *testing.T) {
	orig := &protocol.EvalRequestMsg{
		RequestID: "req-1",
		RepoName:  "repo",
		CommitID:  "commit",
		ZipData:   []byte{0x01, 0x02},
	}
	full := orig.MarshalBytes()

	// Try every possible truncation.
	for i := 0; i < len(full)-1; i++ {
		var m protocol.EvalRequestMsg
		if err := m.UnmarshalBytes(full[:i]); err == nil {
			t.Errorf("expected error for truncation at %d/%d bytes", i, len(full))
		}
	}
}

func TestEvalResultMsgRoundTrip(t *testing.T) {
	orig := &protocol.EvalResultMsg{
		RequestID: "req-42",
		Errors: []protocol.EvalError{
			{
				Message: "field not allowed: foo",
				Coordinates: []protocol.FileCoordinate{
					{Path: "pkg/config.cue", ByteOffset: 123},
					{Path: "pkg/other.cue", ByteOffset: 456},
				},
			},
			{
				Message:     "some repo-level error",
				Coordinates: nil, // empty coordinates
			},
		},
	}

	data := orig.MarshalBytes()

	var decoded protocol.EvalResultMsg
	if err := decoded.UnmarshalBytes(data); err != nil {
		t.Fatalf("UnmarshalBytes: %v", err)
	}

	if decoded.RequestID != orig.RequestID {
		t.Errorf("RequestID: got %q, want %q", decoded.RequestID, orig.RequestID)
	}
	if len(decoded.Errors) != len(orig.Errors) {
		t.Fatalf("Errors length: got %d, want %d", len(decoded.Errors), len(orig.Errors))
	}
	for i, oe := range orig.Errors {
		de := decoded.Errors[i]
		if de.Message != oe.Message {
			t.Errorf("Errors[%d].Message: got %q, want %q", i, de.Message, oe.Message)
		}
		if len(de.Coordinates) != len(oe.Coordinates) {
			t.Fatalf("Errors[%d].Coordinates length: got %d, want %d", i, len(de.Coordinates), len(oe.Coordinates))
		}
		for j, oc := range oe.Coordinates {
			dc := de.Coordinates[j]
			if dc.Path != oc.Path {
				t.Errorf("Errors[%d].Coordinates[%d].Path: got %q, want %q", i, j, dc.Path, oc.Path)
			}
			if dc.ByteOffset != oc.ByteOffset {
				t.Errorf("Errors[%d].Coordinates[%d].ByteOffset: got %d, want %d", i, j, dc.ByteOffset, oc.ByteOffset)
			}
		}
	}
}

func TestEvalResultMsgNoErrors(t *testing.T) {
	orig := &protocol.EvalResultMsg{
		RequestID: "ok-1",
		Errors:    nil,
	}

	data := orig.MarshalBytes()

	var decoded protocol.EvalResultMsg
	if err := decoded.UnmarshalBytes(data); err != nil {
		t.Fatalf("UnmarshalBytes: %v", err)
	}

	if decoded.RequestID != orig.RequestID {
		t.Errorf("RequestID: got %q, want %q", decoded.RequestID, orig.RequestID)
	}
	if len(decoded.Errors) != 0 {
		t.Errorf("Errors: got length %d, want 0", len(decoded.Errors))
	}
}

func TestEvalResultMsgRejectsTrailingBytes(t *testing.T) {
	orig := &protocol.EvalResultMsg{RequestID: "x", Errors: nil}
	data := append(orig.MarshalBytes(), 0x00)

	var m protocol.EvalResultMsg
	if err := m.UnmarshalBytes(data); err == nil {
		t.Fatal("expected error for trailing bytes")
	}
}

func TestEvalResultMsgRejectsTruncated(t *testing.T) {
	orig := &protocol.EvalResultMsg{
		RequestID: "req-1",
		Errors: []protocol.EvalError{
			{
				Message: "err",
				Coordinates: []protocol.FileCoordinate{
					{Path: "a.cue", ByteOffset: 10},
				},
			},
		},
	}
	full := orig.MarshalBytes()

	for i := 0; i < len(full)-1; i++ {
		var m protocol.EvalResultMsg
		if err := m.UnmarshalBytes(full[:i]); err == nil {
			t.Errorf("expected error for truncation at %d/%d bytes", i, len(full))
		}
	}
}

func TestEvalFinishedMsgRoundTrip(t *testing.T) {
	orig := &protocol.EvalFinishedMsg{
		RequestID: "req-42",
	}

	data := orig.MarshalBytes()

	var decoded protocol.EvalFinishedMsg
	if err := decoded.UnmarshalBytes(data); err != nil {
		t.Fatalf("UnmarshalBytes: %v", err)
	}

	if decoded.RequestID != orig.RequestID {
		t.Errorf("RequestID: got %q, want %q", decoded.RequestID, orig.RequestID)
	}
}

func TestEvalFinishedMsgEmptyRequestID(t *testing.T) {
	orig := &protocol.EvalFinishedMsg{
		RequestID: "",
	}

	data := orig.MarshalBytes()

	var decoded protocol.EvalFinishedMsg
	if err := decoded.UnmarshalBytes(data); err != nil {
		t.Fatalf("UnmarshalBytes: %v", err)
	}

	if decoded.RequestID != "" {
		t.Errorf("RequestID: got %q, want %q", decoded.RequestID, "")
	}
}

func TestEvalFinishedMsgRejectsTrailingBytes(t *testing.T) {
	orig := &protocol.EvalFinishedMsg{RequestID: "x"}
	data := append(orig.MarshalBytes(), 0x00)

	var m protocol.EvalFinishedMsg
	if err := m.UnmarshalBytes(data); err == nil {
		t.Fatal("expected error for trailing bytes")
	}
}

func TestEvalFinishedMsgRejectsWrongType(t *testing.T) {
	var m protocol.EvalFinishedMsg
	if err := m.UnmarshalBytes([]byte{0x01}); err == nil {
		t.Fatal("expected error for wrong type byte")
	}
}

func TestEvalFinishedMsgRejectsEmpty(t *testing.T) {
	var m protocol.EvalFinishedMsg
	if err := m.UnmarshalBytes(nil); err == nil {
		t.Fatal("expected error for empty input")
	}
}

func TestEvalFinishedMsgRejectsTruncated(t *testing.T) {
	orig := &protocol.EvalFinishedMsg{
		RequestID: "req-1",
	}
	full := orig.MarshalBytes()

	// Try every possible truncation.
	for i := 0; i < len(full)-1; i++ {
		var m protocol.EvalFinishedMsg
		if err := m.UnmarshalBytes(full[:i]); err == nil {
			t.Errorf("expected error for truncation at %d/%d bytes", i, len(full))
		}
	}
}

func TestPeekMessageType(t *testing.T) {
	tests := []struct {
		name string
		data []byte
		want byte
	}{
		{"changed", (&protocol.ChangedMsg{}).MarshalBytes(), protocol.MsgTypeChanged},
		{"eval request", (&protocol.EvalRequestMsg{}).MarshalBytes(), protocol.MsgTypeEvalRequest},
		{"eval result", (&protocol.EvalResultMsg{}).MarshalBytes(), protocol.MsgTypeEvalResult},
		{"eval finished", (&protocol.EvalFinishedMsg{}).MarshalBytes(), protocol.MsgTypeEvalFinished},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := protocol.PeekMessageType(tt.data)
			if err != nil {
				t.Fatalf("MessageType: %v", err)
			}
			if got != tt.want {
				t.Errorf("MessageType: got 0x%02x, want 0x%02x", got, tt.want)
			}
		})
	}

	if _, err := protocol.PeekMessageType(nil); err == nil {
		t.Fatal("expected error for empty data")
	}
}
