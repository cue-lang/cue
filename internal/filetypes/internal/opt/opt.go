// Copyright 2025 CUE Authors
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

package opt

type Opt[T any] struct {
	x       T
	present bool
}

func (o Opt[T]) IsPresent() bool {
	return o.present
}

func (o Opt[T]) Value() T {
	return o.x
}

func Some[T any](x T) Opt[T] {
	return Opt[T]{
		x:       x,
		present: true,
	}
}

func None[T any]() Opt[T] {
	return Opt[T]{}
}
