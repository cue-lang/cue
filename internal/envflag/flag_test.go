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

type testTypes struct {
	StringDefaultFoo string `envflag:"default:foo"`
	IntDefault5      int    `envflag:"default:5"`
}

type deprecatedFlags struct {
	Foo bool `envflag:"deprecated"`
	Bar bool `envflag:"deprecated,default:true"`
}

func success[T comparable](want T) func(t *testing.T) {
	return func(t *testing.T) {
		var x T
		err := Init(&x, "TEST_VAR")
		qt.Assert(t, qt.IsNil(err))
		qt.Assert(t, qt.Equals(x, want))
	}
}

func failure[T comparable](want T, wantError string) func(t *testing.T) {
	return func(t *testing.T) {
		var x T
		err := Init(&x, "TEST_VAR")
		qt.Assert(t, qt.ErrorMatches(err, wantError))
		qt.Assert(t, qt.Equals(x, want))
	}
}

func invalid[T comparable](want T) func(t *testing.T) {
	return func(t *testing.T) {
		var x T
		err := Init(&x, "TEST_VAR")
		qt.Assert(t, qt.ErrorIs(err, ErrInvalid))
		qt.Assert(t, qt.Equals(x, want))
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
	testName: "JustCommas",
	envVal:   ",,",
	test: success(testFlags{
		DefaultTrue: true,
	}),
}, {
	testName: "Unknown",
	envVal:   "ratchet",
	test: failure(testFlags{DefaultTrue: true},
		"cannot parse TEST_VAR: unknown flag \"ratchet\""),
}, {
	testName: "Set",
	envVal:   "foo",
	test: success(testFlags{
		Foo:         true,
		DefaultTrue: true,
	}),
}, {
	testName: "SetExtraCommas",
	envVal:   ",foo,",
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
	test: failure(testFlags{
		Foo:         true,
		DefaultTrue: true,
	}, "cannot parse TEST_VAR: unknown flag \"other\""),
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
}, {
	testName: "MultipleUnknown",
	envVal:   "other1,other2,foo",
	test: failure(testFlags{
		Foo:         true,
		DefaultTrue: true,
	}, "cannot parse TEST_VAR: unknown flag \"other1\"\nunknown flag \"other2\""),
}, {
	testName: "InvalidIntForBool",
	envVal:   "foo=2",
	test:     invalid(testFlags{DefaultTrue: true}),
}, {
	testName: "StringValue",
	envVal:   "stringdefaultfoo=bar",
	test: success(testTypes{
		StringDefaultFoo: "bar",
		IntDefault5:      5,
	}),
}, {
	testName: "StringEmpty",
	envVal:   "stringdefaultfoo=",
	test: success(testTypes{
		StringDefaultFoo: "",
		IntDefault5:      5,
	}),
}, {
	testName: "FailureStringAlone",
	envVal:   "stringdefaultfoo",
	test: failure(testTypes{
		StringDefaultFoo: "foo",
		IntDefault5:      5,
	}, "cannot parse TEST_VAR: value needed for string flag \"stringdefaultfoo\""),
}, {
	testName: "IntValue",
	envVal:   "intdefault5=123",
	test: success(testTypes{
		StringDefaultFoo: "foo",
		IntDefault5:      123,
	}),
}, {
	testName: "IntEmpty",
	envVal:   "intdefault5=",
	test: invalid(testTypes{
		StringDefaultFoo: "foo",
		IntDefault5:      5,
	}),
}, {
	testName: "DeprecatedWithFalseDefault",
	envVal:   "foo=1",
	test: failure(deprecatedFlags{
		Bar: true,
	}, `cannot parse TEST_VAR: cannot change default value of deprecated flag "foo"`),
}, {
	testName: "DeprecatedNoopWhenSameAndFalseDefault",
	envVal:   "foo=false",
	test: success(deprecatedFlags{
		Bar: true,
	}),
}, {
	testName: "DeprecatedWithTrueDefault",
	envVal:   "bar=0",
	test: failure(deprecatedFlags{
		Bar: true,
	}, `cannot parse TEST_VAR: cannot change default value of deprecated flag "bar"`),
}, {
	testName: "DeprecatedNoopWhenSameAndTrueDefault",
	envVal:   "bar=1",
	test: success(deprecatedFlags{
		Bar: true,
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
