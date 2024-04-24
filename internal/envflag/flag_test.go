package envflag

import (
	"testing"

	"github.com/go-quicktest/qt"
)

type testFlags struct {
	Foo    bool
	BarBaz bool

	DefaultFalse bool `envflag:"default:false"`
	DefaultTrue  bool `envflag:"default:true"`
}

func success[T comparable](want T) func(t *testing.T) {
	return func(t *testing.T) {
		var x T
		err := Init(&x, "TEST_VAR")
		qt.Assert(t, qt.IsNil(err))
		qt.Assert(t, qt.Equals(x, want))
	}
}

func failure[T comparable](wantError string) func(t *testing.T) {
	return func(t *testing.T) {
		var x T
		err := Init(&x, "TEST_VAR")
		qt.Assert(t, qt.ErrorMatches(err, wantError))
	}
}

var tests = []struct {
	testName string
	envVal   string
	test     func(t *testing.T)
}{{
	testName: "Empty",
	envVal:   "",
	test: success(testFlags{
		DefaultTrue: true,
	}),
}, {
	testName: "Unknown",
	envVal:   "ratchet",
	test:     failure[testFlags]("unknown TEST_VAR ratchet"),
}, {
	testName: "Set",
	envVal:   "foo",
	test: success(testFlags{
		Foo:         true,
		DefaultTrue: true,
	}),
}, {
	testName: "SetTwice",
	envVal:   "foo,foo",
	test: success(testFlags{
		Foo:         true,
		DefaultTrue: true,
	}),
}, {
	testName: "SetWithUnknown",
	envVal:   "foo,other",
	test:     failure[testFlags]("unknown TEST_VAR other"),
}, {
	testName: "TwoFlags",
	envVal:   "barbaz,foo",
	test: success(testFlags{
		Foo:         true,
		BarBaz:      true,
		DefaultTrue: true,
	}),
}, {
	testName: "ToggleDefaultFieldsNumeric",
	envVal:   "defaulttrue=0,defaultfalse=1",
	test: success(testFlags{
		DefaultFalse: true,
	}),
}, {
	testName: "ToggleDefaultFieldsWords",
	envVal:   "defaulttrue=false,defaultfalse=true",
	test: success(testFlags{
		DefaultFalse: true,
	}),
}}

func TestInit(t *testing.T) {
	for _, test := range tests {
		t.Run(test.testName, func(t *testing.T) {
			t.Setenv("TEST_VAR", test.envVal)
			test.test(t)
		})
	}
}
