package cuedebug

import (
	"testing"

	"github.com/go-quicktest/qt"
)

type testFlags struct {
	Foo    bool
	BarBaz bool

	DefaultFalse bool `cuedebug:"default:false"`
	DefaultTrue  bool `cuedebug:"default:true"`
}

type testTypes struct {
	IntDefault5 int `cuedebug:"default:5"`
}

func success[T comparable](want T) func(t *testing.T, env string) {
	return func(t *testing.T, env string) {
		var x T
		err := parse(&x, env)
		qt.Assert(t, qt.IsNil(err))
		qt.Assert(t, qt.Equals(x, want))
	}
}

func failure[T comparable](want T, wantError string) func(t *testing.T, env string) {
	return func(t *testing.T, env string) {
		var x T
		err := parse(&x, env)
		qt.Assert(t, qt.ErrorMatches(err, wantError))
		qt.Assert(t, qt.Equals(x, want))
	}
}

var parseTests = []struct {
	testName string
	envVal   string
	test     func(t *testing.T, env string)
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
		"unknown flag \"ratchet\""),
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
	}, "unknown flag \"other\""),
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
	}, "unknown flag \"other1\"\nunknown flag \"other2\""),
}, {
	testName: "InvalidIntForBool",
	envVal:   "foo=2",
	test: failure(testFlags{DefaultTrue: true},
		`invalid bool value for foo: .*`),
}, {
	testName: "IntValue",
	envVal:   "intdefault5=123",
	test:     success(testTypes{IntDefault5: 123}),
}, {
	testName: "IntEmpty",
	envVal:   "intdefault5=",
	test: failure(testTypes{IntDefault5: 5},
		`invalid int value for intdefault5: .*`),
}}

func TestParse(t *testing.T) {
	for _, test := range parseTests {
		t.Run(test.testName, func(t *testing.T) {
			test.test(t, test.envVal)
		})
	}
}
