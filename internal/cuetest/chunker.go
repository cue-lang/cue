// Copyright 2019 CUE Authors
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

package cuetest

import (
	"bytes"
	"testing"
)

// A Chunker is used to find segments in text.
type Chunker struct {
	t *testing.T
	b []byte
	s []byte
	p int
}

// NewChunker returns a new chunker.
func NewChunker(t *testing.T, b []byte) *Chunker {
	return &Chunker{t: t, b: b}
}

// Next finds the first occurrence from the current scan position of beg,
// records the segment from that position until the first occurrence of end
// and then updates the current position. It reports whether a segment enclosed
// by beg and end can be found.
func (c *Chunker) Next(beg, end string) bool {
	if !c.Find(beg) {
		return false
	}
	if !c.Find(end) {
		c.t.Fatalf("quotes at position %d not terminated", c.p)
	}
	return true
}

// Text returns the text segment captured by the last call to Next or Find.
func (c *Chunker) Text() string {
	return string(c.s)
}

// Bytes returns the segment captured by the last call to Next or Find.
func (c *Chunker) Bytes() []byte {
	return c.s
}

// Find searches for key from the current position and sets the current segment
// to the text from current position up till the key's position. If successful,
// the position is updated to point directly after the occurrence of key.
func (c *Chunker) Find(key string) bool {
	p := bytes.Index(c.b, []byte(key))
	if p == -1 {
		c.s = c.b
		return false
	}
	c.p += p + len(key)
	b := c.b
	c.s = b[:p]
	c.b = b[p+len(key):]
	return true
}
