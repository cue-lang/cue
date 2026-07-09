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

package cue

import (
	"fmt"
	"strings"
)

// An Option alters the behavior of methods that accept it. Not every
// option is supported by every method; unlike v1, a method reports an
// error when given an option it does not support.
type Option func(o *options)

// optionFlag identifies which option constructors were applied, so
// that methods can report unsupported options instead of silently
// ignoring them.
type optionFlag uint16

const (
	optConcrete optionFlag = 1 << iota
	optFinal
	optAll
	optDocs
	optAttributes
	optDefinitions
	optHidden
	optOptional
	optInlineImports
)

var optionNames = []struct {
	flag optionFlag
	name string
}{
	{optConcrete, "Concrete"},
	{optFinal, "Final"},
	{optAll, "All"},
	{optDocs, "Docs"},
	{optAttributes, "Attributes"},
	{optDefinitions, "Definitions"},
	{optHidden, "Hidden"},
	{optOptional, "Optional"},
	{optInlineImports, "InlineImports"},
}

type options struct {
	used optionFlag

	concrete        bool
	final           bool
	hasHidden       bool
	omitHidden      bool
	omitDefinitions bool
	omitOptional    bool
	omitAttrs       bool
	docs            bool
	inlineImports   bool
}

func (o *options) updateOptions(opts []Option) {
	for _, fn := range opts {
		fn(o)
	}
}

// check reports an error if any option outside allowed was applied.
func (o *options) check(method string, allowed optionFlag) error {
	bad := o.used &^ allowed
	if bad == 0 {
		return nil
	}
	var names []string
	for _, e := range optionNames {
		if bad&e.flag != 0 {
			names = append(names, e.name)
		}
	}
	return fmt.Errorf("cue: option(s) %s not supported by %s", strings.Join(names, ", "), method)
}

// Concrete ensures that all values are concrete.
//
// For [Value.Validate] this means it returns an error if this is not
// the case. In other cases a non-concrete value will be replaced with
// an error.
func Concrete(concrete bool) Option {
	return func(o *options) {
		o.used |= optConcrete
		if concrete {
			o.concrete = true
			o.final = true
			if !o.hasHidden {
				o.omitHidden = true
				o.omitDefinitions = true
			}
		}
	}
}

// Final indicates a value is final. It implicitly closes all structs
// and lists in a value and selects defaults.
func Final() Option {
	return func(o *options) {
		o.used |= optFinal
		o.final = true
		o.omitDefinitions = true
		o.omitOptional = true
		o.omitHidden = true
	}
}

// All indicates that all fields and values should be included in
// processing even if they can be elided or omitted.
func All() Option {
	return func(o *options) {
		o.used |= optAll
		o.omitAttrs = false
		o.omitHidden = false
		o.omitDefinitions = false
		o.omitOptional = false
	}
}

// Docs indicates whether docs should be included.
func Docs(include bool) Option {
	return func(o *options) {
		o.used |= optDocs
		o.docs = include
	}
}

// Attributes indicates that attributes should be included.
func Attributes(include bool) Option {
	return func(o *options) {
		o.used |= optAttributes
		o.omitAttrs = !include
	}
}

// Definitions indicates whether definitions should be included.
func Definitions(include bool) Option {
	return func(o *options) {
		o.used |= optDefinitions
		o.hasHidden = true
		o.omitDefinitions = !include
	}
}

// Hidden indicates that definitions and hidden fields should be
// included.
func Hidden(include bool) Option {
	return func(o *options) {
		o.used |= optHidden
		o.hasHidden = true
		o.omitHidden = !include
		o.omitDefinitions = !include
	}
}

// Optional indicates that optional fields should be included.
func Optional(include bool) Option {
	return func(o *options) {
		o.used |= optOptional
		o.omitOptional = !include
	}
}

// InlineImports causes references to values within imported packages to
// be inlined. References to builtin packages are not inlined.
func InlineImports(expand bool) Option {
	return func(o *options) {
		o.used |= optInlineImports
		o.inlineImports = expand
	}
}
