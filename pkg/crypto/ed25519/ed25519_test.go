// Copyright 2021 CUE Authors
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

package ed25519_test

import (
	"bytes"
	"crypto/ed25519"
	"fmt"
	"os"
	"testing"

	"cuelang.org/go/cue/format"
	"cuelang.org/go/internal/cuetest"
	"cuelang.org/go/pkg/internal/builtintest"
)

func TestBuiltin(t *testing.T) {
	if cuetest.UpdateGoldenFiles {
		updateGoldenFiles(t)
	}

	builtintest.Run("ed25519", t)
}

// updateGoldenFiles generates deterministic test input.
func updateGoldenFiles(t *testing.T) {
	key := ed25519.NewKeyFromSeed(make([]byte, ed25519.SeedSize))

	var inputs bytes.Buffer
	fmt.Fprintln(&inputs, `import "encoding/hex"`)
	fmt.Fprintln(&inputs, `import "crypto/ed25519"`)

	testCases := []struct {
		key       ed25519.PublicKey
		message   []byte
		signature []byte
	}{
		{
			key:       key.Public().(ed25519.PublicKey),
			message:   []byte("valid"),
			signature: ed25519.Sign(key, []byte("valid")),
		},
		{
			key:       key.Public().(ed25519.PublicKey),
			message:   []byte("message mismatch"),
			signature: ed25519.Sign(key, []byte("mismatching message")),
		},
		{
			key:       make(ed25519.PublicKey, ed25519.PublicKeySize),
			message:   []byte("wrong key"),
			signature: ed25519.Sign(key, []byte("wrong key")),
		},
		{
			key:       ed25519.PublicKey{},
			message:   []byte("wrong key size"),
			signature: ed25519.Sign(key, []byte("wrong key size")),
		},
	}
	for i, tc := range testCases {
		fmt.Fprintf(
			&inputs,
			"t%d: ed25519.Valid(hex.Decode(\"%x\"), hex.Decode(\"%x\"), hex.Decode(\"%x\"))\n",
			i, tc.key, tc.message, tc.signature,
		)
	}

	fInputs, err := format.Source(inputs.Bytes())
	if err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	fmt.Fprintln(&buf, "-- in.cue --")
	fmt.Fprintf(&buf, "%s", fInputs)
	if err := os.WriteFile("testdata/gen.txtar", buf.Bytes(), 0666); err != nil {
		t.Fatal(err)
	}
}
