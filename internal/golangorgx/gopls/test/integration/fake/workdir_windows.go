// SPDX-License-Identifier: BSD-3-Clause
// Copyright 2021 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Derived from
// golang.org/x/tools/gopls/internal/test/integration/fake/workdir_windows.go
// at revision c11269ccb0bf.

package fake

import (
	"errors"
	"syscall"
)

func init() {
	// constants copied from GOROOT/src/internal/syscall/windows/syscall_windows.go
	const (
		ERROR_LOCK_VIOLATION syscall.Errno = 33
	)

	isWindowsErrLockViolation = func(err error) bool {
		return errors.Is(err, ERROR_LOCK_VIOLATION)
	}
}
