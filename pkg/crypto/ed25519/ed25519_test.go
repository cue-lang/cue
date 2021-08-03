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
	"flag"
	"fmt"
	"os"
	"testing"

	"cuelang.org/go/pkg/internal/builtintest"
)

var update = flag.Bool("update", false, "Update the golden files")

func TestBuiltin(t *testing.T) {
	if *update {
		updateGoldenFiles(t)
	}

	builtintest.Run("ed25519", t)
}

func updateGoldenFiles(t *testing.T) {
	key := ed25519.NewKeyFromSeed(make([]byte, ed25519.SeedSize))

	var inputs, result, errors bytes.Buffer
	fmt.Fprintln(&inputs, `import "encoding/hex"`)
	fmt.Fprintln(&inputs, `import "crypto/ed25519"`)
	fmt.Fprintln(&result, "Result:")
	fmt.Fprintln(&errors, "Errors:")

	testCases := []struct {
		key       ed25519.PublicKey
		message   []byte
		signature []byte
		expected  string
		err       string
	}{
		{
			key:       key.Public().(ed25519.PublicKey),
			message:   []byte("valid"),
			signature: ed25519.Sign(key, []byte("valid")),
			expected:  "true",
		},
		{
			key:       key.Public().(ed25519.PublicKey),
			message:   []byte("message mismatch"),
			signature: ed25519.Sign(key, []byte("mismatching message")),
			expected:  "false",
		},
		{
			key:       make(ed25519.PublicKey, ed25519.PublicKeySize),
			message:   []byte("wrong key"),
			signature: ed25519.Sign(key, []byte("wrong key")),
			expected:  "false",
		},
		{
			key:       ed25519.PublicKey{},
			message:   []byte("wrong key size"),
			signature: ed25519.Sign(key, []byte("wrong key size")),
			err:       "error in call to crypto/ed25519.Verify: ed25519: publicKey must be 32 bytes",
		},
	}
	for i, tc := range testCases {
		fmt.Fprintf(
			&inputs,
			"t%d: ed25519.Verify(hex.Decode(\"%x\"), hex.Decode(\"%x\"), hex.Decode(\"%x\"))\n",
			i, tc.key, tc.message, tc.signature,
		)
		if tc.err == "" {
			fmt.Fprintf(&result, "t%d: %s\n", i, tc.expected)
		} else {
			fmt.Fprintf(&result, "t%d: _|_ // %s\n", i, tc.err)
			fmt.Fprintf(&errors, "%s:\n    ./in.cue:%d:5\n", tc.err, i+3)
		}
	}

	var buf bytes.Buffer
	fmt.Fprintln(&buf, "# go test ./pkg/crypto/ed25519 -update")
	fmt.Fprintln(&buf, "-- in.cue --")
	fmt.Fprintln(&buf, inputs.String())
	fmt.Fprintln(&buf, "-- out/ed25519 --")
	fmt.Fprintln(&buf, errors.String())
	fmt.Fprintln(&buf, result.String())
	if err := os.WriteFile("testdata/gen.txtar", buf.Bytes(), 0666); err != nil {
		t.Fatal(err)
	}
}
