// Copyright 2019 CUE Authors
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

package os

//go:generate go run gen.go

import (
	"fmt"
	"os"
	"strings"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/parser"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/internal/task"
)

func init() {
	task.Register("tool/os.Setenv", newSetenvCmd)
	task.Register("tool/os.Getenv", newGetenvCmd)
	task.Register("tool/os.Environ", newEnvironCmd)
	task.Register("tool/os.Clearenv", newClearenvCmd)

	// TODO:
	// Tasks:
	// - Exit?
	// - Getwd/ Setwd (or in tool/file?)

	// Functions:
	// - Hostname
	// - UserCache/Home/Config (or in os/user?)
}

type clearenvCmd struct{}

func newClearenvCmd(v cue.Value) (task.Runner, error) {
	return &clearenvCmd{}, nil
}

func (c *clearenvCmd) Run(ctx *task.Context, v cue.Value) (res interface{}, err error) {
	os.Clearenv()
	return map[string]interface{}{}, nil
}

type setenvCmd struct{}

func newSetenvCmd(v cue.Value) (task.Runner, error) {
	return &setenvCmd{}, nil
}

func (c *setenvCmd) Run(ctx *task.Context, v cue.Value) (res interface{}, err error) {
	iter, err := v.Fields()
	if err != nil {
		return nil, err
	}

	for iter.Next() {
		name := iter.Label()
		if strings.HasPrefix(name, "$") {
			continue
		}

		v, _ := iter.Value().Default()

		if !v.IsConcrete() {
			return nil, errors.Newf(v.Pos(),
				"non-concrete environment variable %s", name)
		}
		switch k := v.IncompleteKind(); k {
		case cue.ListKind, cue.StructKind:
			return nil, errors.Newf(v.Pos(),
				"unsupported type %s for environment variable %s", k, name)

		case cue.NullKind:
			err = os.Unsetenv(name)

		case cue.BoolKind:
			if b, _ := v.Bool(); b {
				err = os.Setenv(name, "1")
			} else {
				err = os.Setenv(name, "0")
			}

		case cue.StringKind:
			s, _ := v.String()
			err = os.Setenv(name, s)

		default:
			err = os.Setenv(name, fmt.Sprint(v))
		}

		if err != nil {
			return nil, err
		}
	}

	return map[string]interface{}{}, err
}

type getenvCmd struct{}

func newGetenvCmd(v cue.Value) (task.Runner, error) {
	return &getenvCmd{}, nil
}

func (c *getenvCmd) Run(ctx *task.Context, v cue.Value) (res interface{}, err error) {
	iter, err := v.Fields()
	if err != nil {
		return nil, err
	}

	update := map[string]interface{}{}

	for iter.Next() {
		name := iter.Label()
		if strings.HasPrefix(name, "$") {
			continue
		}
		v := iter.Value()

		if err := validateEntry(name, v); err != nil {
			return nil, err
		}

		str, ok := os.LookupEnv(name)
		if !ok {
			update[name] = nil
			continue
		}
		x, err := fromString(name, str, v)
		if err != nil {
			return nil, err
		}
		update[name] = x
	}

	return update, nil
}

type environCmd struct{}

func newEnvironCmd(v cue.Value) (task.Runner, error) {
	return &environCmd{}, nil
}

func (c *environCmd) Run(ctx *task.Context, v cue.Value) (res interface{}, err error) {
	iter, err := v.Fields()
	if err != nil {
		return nil, err
	}

	update := map[string]interface{}{}

	for _, kv := range os.Environ() {
		a := strings.SplitN(kv, "=", 2)

		name := a[0]
		str := a[1]

		if v := v.Lookup(name); v.Exists() {
			update[name], err = fromString(name, str, v)
			if err != nil {
				return nil, err
			}
		} else {
			update[name] = str
		}
	}

	for iter.Next() {
		name := iter.Label()
		if strings.HasPrefix(name, "$") {
			continue
		}
		if err := validateEntry(name, iter.Value()); err != nil {
			return nil, err
		}
		if _, ok := update[name]; !ok {
			update[name] = nil
		}
	}

	return update, nil
}

func validateEntry(name string, v cue.Value) error {
	if k := v.IncompleteKind(); k&^(cue.NumberKind|cue.NullKind|cue.BoolKind|cue.StringKind) != 0 {
		return errors.Newf(v.Pos(),
			"invalid type %s for environment variable %s", k, name)
	}
	return nil
}

func fromString(name, str string, v cue.Value) (x interface{}, err error) {
	k := v.IncompleteKind()

	var expr ast.Expr
	var errs errors.Error

	if k&cue.NumberKind != 0 {
		expr, err = parser.ParseExpr(name, str)
		if err != nil {
			errs = errors.Wrapf(err, v.Pos(),
				"invalid number for environment variable %s", name)
		}
	}

	if k&cue.BoolKind != 0 {
		str = strings.TrimSpace(str)
		b, ok := boolValues[str]
		if !ok {
			errors.Append(errs, errors.Newf(v.Pos(),
				"invalid boolean value %q for environment variable %s", str, name))
		} else if expr != nil || k&cue.StringKind != 0 {
			// Convert into an expression
			bl := ast.NewBool(b)
			if expr != nil {
				expr = &ast.BinaryExpr{Op: token.OR, X: expr, Y: bl}
			} else {
				expr = bl
			}
		} else {
			x = b
		}
	}

	if k&cue.StringKind != 0 {
		if expr != nil {
			expr = &ast.BinaryExpr{Op: token.OR, X: expr, Y: ast.NewString(str)}
		} else {
			x = str
		}
	}

	switch {
	case expr != nil:
		return expr, nil
	case x != nil:
		return x, nil
	case errs == nil:
		return nil, errors.Newf(v.Pos(),
			"invalid type for environment variable %s", name)
	}
	return nil, errs
}

var boolValues = map[string]bool{
	"1":     true,
	"0":     false,
	"t":     true,
	"f":     false,
	"T":     true,
	"F":     false,
	"true":  true,
	"false": false,
	"TRUE":  true,
	"FALSE": false,
	"True":  true,
	"False": false,
}
