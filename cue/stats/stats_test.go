package stats

import (
	"fmt"
	"math/rand/v2"
	"reflect"
	"strings"
	"testing"

	"github.com/go-quicktest/qt"

	"cuelang.org/go/internal"
)

var zero = Counts{EvalVersion: internal.EvalV3}

func TestStatsArithmetic(t *testing.T) {
	// Minimal smoke test to catch fields we might have
	// added but forgotten to implement arithmetic for.
	s1 := zero
	s2 := randCounts()
	s1.Add(s2)
	qt.Assert(t, qt.Equals(s1, s2))

	diff := withZeroMax(s2.Since(s1))
	qt.Assert(t, qt.Equals(diff, zero))
}

func TestStatsString(t *testing.T) {
	// Smoke test that the string form mentions all the fields.
	s := randCounts().String()
	ct := reflect.TypeFor[Counts]()
	for i := range ct.NumField() {
		name := ct.Field(i).Name
		switch name {
		case "EvalVersion":
			continue
		case "Retained":
			// Special case for Retained for some reason.
			name = "Retain"
		}
		if !strings.Contains(s, name) {
			t.Errorf("string does not mention field %q", name)
		}
	}
}

// randCounts sets all the counts to random values >= 2.
func randCounts() Counts {
	s := new(Counts)
	sv := reflect.ValueOf(s).Elem()
	for i := range sv.NumField() {
		f := sv.Field(i).Addr().Interface()
		switch f := f.(type) {
		case *int64:
			*f = rand.Int64N(1000000) + 2
		case *internal.EvaluatorVersion:
			*f = zero.EvalVersion
		default:
			panic(fmt.Errorf("unexpected field type at field %d", i))
		}
	}
	return *s
}

func withZeroMax(c Counts) Counts {
	v := reflect.ValueOf(&c).Elem()
	t := v.Type()
	for i := range t.NumField() {
		if strings.HasPrefix(t.Field(i).Name, "Max") {
			f := v.Field(i).Addr().Interface().(*int64)
			*f = 0
		}
	}
	return c
}
