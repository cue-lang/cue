// Copyright 2026 The CUE Authors
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

package lsprpc

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDialNetCanceled(t *testing.T) {
	// A context which is done during the retries stops them
	// rather than waiting for all of them.
	addr := filepath.Join(t.TempDir(), "missing.sock")
	d, err := newAutoDialer("unix;"+addr, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, err := d.dialNet(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("got error %v, want %v", err, context.DeadlineExceeded)
	}
	// All the retries take about 13s.
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("dialing took %v, want it to stop at the deadline", elapsed)
	}

	// A canceled context does not make a running daemon look absent,
	// so its socket is kept and no other daemon is started.
	addr = filepath.Join(t.TempDir(), "daemon.sock")
	l, err := net.Listen("unix", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	d = &autoDialer{
		network: "unix",
		addr:    addr,
		isAuto:  true,
		argFunc: func(network, addr string) []string {
			t.Errorf("started a daemon at %s %s", network, addr)
			return nil
		},
	}
	ctx, cancel = context.WithCancel(t.Context())
	cancel()
	if _, err := d.dialNet(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("got error %v, want %v", err, context.Canceled)
	}
	if _, err := os.Stat(addr); err != nil {
		t.Fatal(err)
	}
}
