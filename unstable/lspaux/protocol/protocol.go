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

// WARNING: THIS PACKAGE IS EXPERIMENTAL.
// ITS API MAY CHANGE AT ANY TIME.
package protocol

import (
	"encoding/binary"
	"fmt"
)

const (
	MsgTypeChanged      byte = 0x01
	MsgTypeEvalRequest  byte = 0x02
	MsgTypeEvalResult   byte = 0x03
	MsgTypeEvalFinished byte = 0x04
)

// PeekMessageType returns the message type byte from a raw message,
// allowing callers to determine which struct to decode into.
func PeekMessageType(data []byte) (byte, error) {
	if len(data) == 0 {
		return 0, fmt.Errorf("empty message")
	}
	return data[0], nil
}

// Encoding / decoding helpers

func appendUint32(buf []byte, v uint32) []byte {
	var b [4]byte
	binary.BigEndian.PutUint32(b[:], v)
	return append(buf, b[:]...)
}

func appendString(buf []byte, s string) []byte {
	buf = appendUint32(buf, uint32(len(s)))
	return append(buf, s...)
}

func appendBytes(buf []byte, data []byte) []byte {
	buf = appendUint32(buf, uint32(len(data)))
	return append(buf, data...)
}

// reader provides sequential decoding from a byte slice.
type reader struct {
	data   []byte
	offset int
}

func (r *reader) remaining() int { return len(r.data) - r.offset }

func (r *reader) readByte() (byte, error) {
	if r.remaining() < 1 {
		return 0, fmt.Errorf("unexpected end of message reading byte at offset %d", r.offset)
	}
	b := r.data[r.offset]
	r.offset++
	return b, nil
}

func (r *reader) readUint32() (uint32, error) {
	if r.remaining() < 4 {
		return 0, fmt.Errorf("unexpected end of message reading uint32 at offset %d", r.offset)
	}
	v := binary.BigEndian.Uint32(r.data[r.offset:])
	r.offset += 4
	return v, nil
}

func (r *reader) readString() (string, error) {
	n, err := r.readUint32()
	if err != nil {
		return "", fmt.Errorf("reading string length: %w", err)
	}
	nInt := int(n)
	if r.remaining() < nInt {
		return "", fmt.Errorf("unexpected end of message reading string of length %d at offset %d (only %d bytes remain)", n, r.offset, r.remaining())
	}
	s := string(r.data[r.offset : r.offset+nInt])
	r.offset += nInt
	return s, nil
}

func (r *reader) readBytes() ([]byte, error) {
	n, err := r.readUint32()
	if err != nil {
		return nil, fmt.Errorf("reading byte array length: %w", err)
	}
	nInt := int(n)
	if r.remaining() < nInt {
		return nil, fmt.Errorf("unexpected end of message reading byte array of length %d at offset %d (only %d bytes remain)", n, r.offset, r.remaining())
	}
	b := make([]byte, n)
	copy(b, r.data[r.offset:r.offset+nInt])
	r.offset += nInt
	return b, nil
}

func (r *reader) expectEmpty(msgName string) error {
	if r.remaining() != 0 {
		return fmt.Errorf("%s: %d unexpected trailing bytes", msgName, r.remaining())
	}
	return nil
}

// ChangedMsg indicates that something has changed and the client may
// wish to request a re-evaluation.
type ChangedMsg struct{}

func (m *ChangedMsg) MarshalBytes() []byte {
	return []byte{MsgTypeChanged}
}

func (m *ChangedMsg) UnmarshalBytes(data []byte) error {
	r := &reader{data: data}

	typ, err := r.readByte()
	if err != nil {
		return fmt.Errorf("changed: %w", err)
	}
	if typ != MsgTypeChanged {
		return fmt.Errorf("changed: wrong message type: expected 0x%02x, got 0x%02x", MsgTypeChanged, typ)
	}

	return r.expectEmpty("changed")
}

// EvalRequestMsg is sent to request an evaluation of the
// configuration for a given repository at a given commit, overlaid
// with local modifications supplied as a zip file.
type EvalRequestMsg struct {
	RequestID string // opaque evaluation-request identifier
	RepoName  string // e.g. "https://cue.gerrithub.io/a/cue-lang/cue"
	CommitID  string // current commit id of the git repo
	ZipData   []byte // zip file of local modifications
}

func (m *EvalRequestMsg) MarshalBytes() []byte {
	buf := []byte{MsgTypeEvalRequest}
	buf = appendString(buf, m.RequestID)
	buf = appendString(buf, m.RepoName)
	buf = appendString(buf, m.CommitID)
	buf = appendBytes(buf, m.ZipData)
	return buf
}

func (m *EvalRequestMsg) UnmarshalBytes(data []byte) error {
	r := &reader{data: data}

	typ, err := r.readByte()
	if err != nil {
		return fmt.Errorf("eval request: %w", err)
	}
	if typ != MsgTypeEvalRequest {
		return fmt.Errorf("eval request: wrong message type: expected 0x%02x, got 0x%02x", MsgTypeEvalRequest, typ)
	}

	if m.RequestID, err = r.readString(); err != nil {
		return fmt.Errorf("eval request: request ID: %w", err)
	}
	if m.RepoName, err = r.readString(); err != nil {
		return fmt.Errorf("eval request: repo name: %w", err)
	}
	if m.CommitID, err = r.readString(); err != nil {
		return fmt.Errorf("eval request: commit ID: %w", err)
	}
	if m.ZipData, err = r.readBytes(); err != nil {
		return fmt.Errorf("eval request: zip data: %w", err)
	}

	return r.expectEmpty("eval request")
}

// FileCoordinate identifies a position within a file.
type FileCoordinate struct {
	// slash-separated path relative to the git repo root. In the
	// future, this could become a URI, with the git repo name as the
	// prefix
	Path       string
	ByteOffset uint32 // byte offset to the start of the token
}

// EvalError represents a single error produced during evaluation.
type EvalError struct {
	Message     string           // human-readable error message
	Coordinates []FileCoordinate // may be empty
}

// EvalResultMsg is sent in response to an EvalRequestMsg. Zero or
// more results are sent per request, matched by RequestID.
type EvalResultMsg struct {
	RequestID string      // echoed from the corresponding EvalRequestMsg
	Errors    []EvalError // may be empty if evaluation succeeded
}

func (m *EvalResultMsg) MarshalBytes() []byte {
	buf := []byte{MsgTypeEvalResult}
	buf = appendString(buf, m.RequestID)

	buf = appendUint32(buf, uint32(len(m.Errors)))
	for _, e := range m.Errors {
		buf = appendString(buf, e.Message)

		buf = appendUint32(buf, uint32(len(e.Coordinates)))
		for _, c := range e.Coordinates {
			buf = appendString(buf, c.Path)
			buf = appendUint32(buf, c.ByteOffset)
		}
	}

	return buf
}

func (m *EvalResultMsg) UnmarshalBytes(data []byte) error {
	r := &reader{data: data}

	typ, err := r.readByte()
	if err != nil {
		return fmt.Errorf("eval result: %w", err)
	}
	if typ != MsgTypeEvalResult {
		return fmt.Errorf("eval result: wrong message type: expected 0x%02x, got 0x%02x", MsgTypeEvalResult, typ)
	}

	if m.RequestID, err = r.readString(); err != nil {
		return fmt.Errorf("eval result: request ID: %w", err)
	}

	numErrors, err := r.readUint32()
	if err != nil {
		return fmt.Errorf("eval result: error count: %w", err)
	}

	m.Errors = make([]EvalError, numErrors)
	for i := range m.Errors {
		evalErr := &m.Errors[i]
		if evalErr.Message, err = r.readString(); err != nil {
			return fmt.Errorf("eval result: error[%d] message: %w", i, err)
		}

		numCoords, err := r.readUint32()
		if err != nil {
			return fmt.Errorf("eval result: error[%d] coordinate count: %w", i, err)
		}

		evalErr.Coordinates = make([]FileCoordinate, numCoords)
		for j := range evalErr.Coordinates {
			coord := &evalErr.Coordinates[j]
			if coord.Path, err = r.readString(); err != nil {
				return fmt.Errorf("eval result: error[%d] coordinate[%d] path: %w", i, j, err)
			}
			if coord.ByteOffset, err = r.readUint32(); err != nil {
				return fmt.Errorf("eval result: error[%d] coordinate[%d] byte offset: %w", i, j, err)
			}
		}
	}

	return r.expectEmpty("eval result")
}

// EvalFinishedMsg is sent in response to an EvalRequestMsg. Exactly
// one finished message is sent per request, matched by RequestID.
type EvalFinishedMsg struct {
	RequestID string // echoed from the corresponding EvalRequestMsg
}

func (m *EvalFinishedMsg) MarshalBytes() []byte {
	buf := []byte{MsgTypeEvalFinished}
	buf = appendString(buf, m.RequestID)

	return buf
}

func (m *EvalFinishedMsg) UnmarshalBytes(data []byte) error {
	r := &reader{data: data}

	typ, err := r.readByte()
	if err != nil {
		return fmt.Errorf("eval finished: %w", err)
	}
	if typ != MsgTypeEvalFinished {
		return fmt.Errorf("eval finished: wrong message type: expected 0x%02x, got 0x%02x", MsgTypeEvalFinished, typ)
	}

	if m.RequestID, err = r.readString(); err != nil {
		return fmt.Errorf("eval finished: request ID: %w", err)
	}

	return r.expectEmpty("eval finished")
}
