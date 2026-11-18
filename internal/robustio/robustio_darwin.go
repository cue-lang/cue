// SPDX-License-Identifier: BSD-3-Clause
// Copyright 2019 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Derived from golang.org/x/tools/internal/robustio/robustio_darwin.go
// at revision c11269ccb0bf.

package robustio

import (
	"errors"
	"syscall"
)

const errFileNotFound = syscall.ENOENT

// isEphemeralError returns true if err may be resolved by waiting.
func isEphemeralError(err error) bool {
	var errno syscall.Errno
	if errors.As(err, &errno) {
		return errno == errFileNotFound
	}
	return false
}
