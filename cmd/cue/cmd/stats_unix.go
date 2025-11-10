// Copyright 2025 The CUE Authors
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

//go:build unix

package cmd

import "syscall"

func getProcStatsSelf(ps *procStats) error {
	var sys syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &sys); err != nil {
		return err
	}

	ps.UserNano = uint64(sys.Utime.Nano())
	ps.SysNano = uint64(sys.Stime.Nano())
	ps.MaxRssBytes = uint64(sys.Maxrss * 1024) // [syscall.Rusage.Maxrss] is in KiB.
	return nil
}
