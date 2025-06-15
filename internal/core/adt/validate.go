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

// Package validate collects errors from an evaluated Vertex.
package adt

type ValidateConfig struct {
	// Concrete, if true, requires that all values be concrete.
	Concrete bool

	// Final, if true, checks that there are no required fields left.
	Final bool

	// DisallowCycles indicates that there may not be cycles.
	DisallowCycles bool

	// AllErrors continues descending into a Vertex, even if errors are found.
	AllErrors bool

	// TODO: omitOptional, if this is becomes relevant.
}

// Validate checks that a value has certain properties. The value must have
// been evaluated.
func Validate(ctx *OpContext, v *Vertex, cfg *ValidateConfig) *Bottom {
	if cfg == nil {
		cfg = &ValidateConfig{}
	}
	x := validator{ValidateConfig: *cfg, ctx: ctx}
	x.validate(v)
	return x.err
}

// validateValue checks that a value has certain properties. The value must have
// been evaluated.
func validateValue(ctx *OpContext, v Value, cfg *ValidateConfig) *Bottom {
	if cfg == nil {
		cfg = &ValidateConfig{}
	}

	if v.Concreteness() > Concrete {
		return &Bottom{
			Code: IncompleteError,
			Err:  ctx.Newf("non-concrete value '%v'", v),
			Node: ctx.vertex,
		}
	}

	if x, ok := v.(*Vertex); ok {
		if v.Kind()&(StructKind|ListKind) != 0 {
			x.Finalize(ctx)
		}
		return Validate(ctx, x, cfg)
	}

	return nil
}

type validator struct {
	ValidateConfig
	ctx          *OpContext
	err          *Bottom
	inDefinition int

	// shared vertices should be visited at least once if referenced by
	// a non-definition.
	// TODO: we could also keep track of the number of references to a
	// shared vertex. This would allow us to report more than a single error
	// per shared vertex.
	visited map[*Vertex]bool
}

func (v *validator) checkConcrete() bool {
	return v.Concrete && v.inDefinition == 0
}

func (v *validator) checkFinal() bool {
	return (v.Concrete || v.Final) && v.inDefinition == 0
}

func (v *validator) add(b *Bottom) {
	if !v.AllErrors {
		v.err = CombineErrors(nil, v.err, b)
		return
	}
	if !b.ChildError {
		v.err = CombineErrors(nil, v.err, b)
	}
}

func (v *validator) validate(x *Vertex) {
	defer v.ctx.PopArcAndLabel(v.ctx.PushArcAndLabel(x))

	y := x

	// Dereference values, but only those that are not shared. This includes let
	// values. This prevents us from processing structure-shared nodes more than
	// once and prevents potential cycles.
	x = x.DerefValue()
	if y != x {
		// Ensure that each structure shared node is processed at least once
		// in a position that is not a definition.
		if v.inDefinition > 0 {
			return
		}
		if v.visited == nil {
			v.visited = make(map[*Vertex]bool)
		}
		if v.visited[x] {
			return
		}
		v.visited[x] = true
	}

	if b := x.Bottom(); b != nil {
		switch b.Code {
		case CycleError:
			if v.checkFinal() || v.DisallowCycles {
				v.add(b)
			}

		case IncompleteError:
			if v.checkFinal() {
				v.add(b)
			}

		default:
			v.add(b)
		}
		if !b.HasRecursive {
			return
		}

	} else if v.checkConcrete() {
		x = x.Default()
		if !IsConcrete(x) {
			x := x.Value()
			v.add(&Bottom{
				Code: IncompleteError,
				Err:  v.ctx.Newf("incomplete value %v", x),
			})
		}
	}

	for _, a := range x.Arcs {
		if a.ArcType == ArcRequired && v.Final && v.inDefinition == 0 {
			v.ctx.PushArcAndLabel(a)
			v.add(NewRequiredNotPresentError(v.ctx, a))
			v.ctx.PopArcAndLabel(a)
			continue
		}

		if a.Label.IsLet() || !a.IsDefined(v.ctx) {
			continue
		}
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
