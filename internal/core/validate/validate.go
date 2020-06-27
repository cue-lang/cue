// Copyright 2020 CUE Authors
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

package validate

import (
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/internal/core/adt"
	"cuelang.org/go/internal/core/debug"
)

type Config struct {
	// Concrete, if true, requires that all values be concrete.
	Concrete bool

	// DisallowCycles indicates that there may not be cycles.
	DisallowCycles bool

	// AllErrors continues descending into a Vertex, even if errors are found.
	AllErrors bool

	// TODO: omitOptional, if this is becomes relevant.
}

// Validate checks that a value has certain properties. The value must have
// been evaluated.
func Validate(r adt.Runtime, v *adt.Vertex, cfg *Config) *adt.Bottom {
	if cfg == nil {
		cfg = &Config{}
	}
	x := validator{Config: *cfg, runtime: r}
	x.validate(v)
	return x.err
}

type validator struct {
	Config
	err          *adt.Bottom
	inDefinition int
	runtime      adt.Runtime
}

func (v *validator) add(b *adt.Bottom) {
	if !v.AllErrors {
		v.err = adt.CombineErrors(nil, v.err, b)
		return
	}
	if !b.ChildError {
		v.err = adt.CombineErrors(nil, v.err, b)
	}
}

func (v *validator) validate(x *adt.Vertex) {
	if b, _ := x.Value.(*adt.Bottom); b != nil {
		switch b.Code {
		case adt.CycleError:
			if v.Concrete || v.DisallowCycles {
				v.add(b)
			}

		case adt.IncompleteError, adt.NotExistError:
			if v.Concrete {
				v.add(b)
			}

		default:
			v.add(b)
		}
		if !b.HasRecursive {
			return
		}

	} else if v.Concrete && v.inDefinition == 0 && !adt.IsConcrete(x) {
		p := token.NoPos
		if src := x.Value.Source(); src != nil {
			p = src.Pos()
		}
		// TODO: use ValueError to get full path.
		v.add(&adt.Bottom{
			Code: adt.IncompleteError,
			Err: errors.Newf(p, "incomplete value %v",
				debug.NodeString(v.runtime, x.Value, nil)),
		})
	}

	for _, a := range x.Arcs {
		if !v.AllErrors && v.err != nil {
			break
		}
		if a.Label.IsRegular() {
			v.validate(a)
		} else {
			v.inDefinition++
			v.validate(a)
			v.inDefinition--
		}
	}
}
