package jsonschema

import (
	"fmt"

	"cuelang.org/go/cue"
)

// matchExpr checks whether a value v's expression matches
// the given arguments according to the following rules:
// - the operation returned by v.Expr must equal wantOp
// - each element in wantArgs is matched in turn with the argument
// slice returned by Expr
// - an element of type *cue.Value is set unconditionally
// - an element of type [valueMatcher] is checked by passing the
// argument to the matchValue method
// - an nil element matches any value.
// - an argument of type [valuesMatcher] is checked by passing it
// all the remain values in the argument slice. This must be
// the last value in wantArgs.
// - an argument of any other type is passed to [Value.Decode];
// the match fails if the decoding fails.
func matchExpr(wantOp cue.Op, wantArgs ...any) valueMatcher {
	e := exprMatcher{
		op: wantOp,
	}
	if len(wantArgs) > 0 {
		if a, ok := wantArgs[len(wantArgs)-1].(valuesMatcher); ok {
			e.varArgs = a
			wantArgs = wantArgs[:len(wantArgs)-1]
		}
	}
	e.args = wantArgs
	return e
}

type exprMatcher struct {
	op      cue.Op
	args    []any
	varArgs valuesMatcher
}

type valueMatcher interface {
	matchValue(v cue.Value) error
}

type valuesMatcher interface {
	matchValues(vs []cue.Value) error
}

type valueMatcherFunc func(v cue.Value) error

func (f valueMatcherFunc) matchValue(v cue.Value) error {
	return f(v)
}

func (m exprMatcher) matchValue(v cue.Value) error {
	op, args := v.Expr()
	if op != m.op {
		return fmt.Errorf("unexpected operator; got %v want %v", op, m.op)
	}
	switch {
	case m.varArgs == nil && len(args) != len(m.args):
		return fmt.Errorf("unexpected arg len; got %v want %v", len(args), len(m.args))
	case len(args) < len(m.args):
		return fmt.Errorf("unexpected arg len; got %v want >%v", len(args), len(m.args))
	}
	for i, arg := range args {
		switch wantArg := m.args[i].(type) {
		case nil:
			continue
		case *cue.Value:
			*wantArg = arg
		case valueMatcher:
			if err := wantArg.matchValue(arg); err != nil {
				return fmt.Errorf("check arg %d failed: %v", i, err)
			}
		default:
			if err := arg.Decode(wantArg); err != nil {
				return fmt.Errorf("cannot decode arg %d: %v", i, err)
			}
		}
	}
	if m.varArgs != nil {
		if err := m.varArgs.matchValues(args[len(m.args):]); err != nil {
			return err
		}
	}
	return nil
}

func matchValue(v cue.Value, errp *error, m valueMatcher) bool {
	err := m.matchValue(v)
	if errp != nil && *errp == nil {
		*errp = err
	}
	return err == nil
}

// matchString returns a checker that checks that
// fmt.Sprint(v) is equal to the given string.
func matchString(want string) valueMatcher {
	return valueMatcherFunc(func(v cue.Value) error {
		got := fmt.Sprintf("%#v", v)
		if got == want {
			return nil
		}
		return fmt.Errorf("check failed; got %q want %q", got, want)
	})
}
