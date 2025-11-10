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

//go:build !unix

package cmd

import "fmt"

//TODO(mvdan): implement for GOOS=windows like the os package does:
// var u syscall.Rusage
// e = syscall.GetProcessTimes(syscall.Handle(handle), &u.CreationTime, &u.ExitTime, &u.KernelTime, &u.UserTime)
// if e != nil {
// 	return nil, NewSyscallError("GetProcessTimes", e)
// }

func getProcStatsSelf(ps *procStats) error {
	return fmt.Errorf("unimplemented")
}
