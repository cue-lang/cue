// Copyright 2022 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build !go1.19

package par

import "sync/atomic"

// atomicBool implements the atomic.Bool type for Go versions before go
// 1.19. It's a copy of the relevant parts of the Go 1.19 atomic.Bool
// code as of commit a4d5fbc3a48b63f19fcd2a4d040a85c75a2709b5.
type atomicBool struct {
	v uint32
}

// Load atomically loads and returns the value stored in x.
func (x *atomicBool) Load() bool { return atomic.LoadUint32(&x.v) != 0 }

// Store atomically stores val into x.
func (x *atomicBool) Store(val bool) { atomic.StoreUint32(&x.v, b32(val)) }

// b32 returns a uint32 0 or 1 representing b.
func b32(b bool) uint32 {
	if b {
		return 1
	}
	return 0
}
