package genstruct

import (
	"fmt"
	"maps"
	"slices"
	"testing"

	"github.com/go-quicktest/qt"
)

func TestEnum(t *testing.T) {
	values := []string{"foo", "bar", "baz"}
	var s Struct
	a := AddEnum(&s, values, "", "values", "string", values)
	qt.Assert(t, qt.Equals(s.Size(), 1))
	data := make([]byte, s.Size())
	a.Put(data, "foo")
	qt.Assert(t, qt.Equals(data[0], 0))
	a.Put(data, "bar")
	qt.Assert(t, qt.Equals(data[0], 1))
	m := IndexMap(values)
	qt.Assert(t, qt.DeepEquals(m, map[string]int{
		"foo": 0,
		"bar": 1,
		"baz": 2,
	}))
	qt.Assert(t, qt.Equals(s.GenInit(), `var (
	values = []string {
		"foo",
		"bar",
		"baz",
	}
	values_rev = genstruct.IndexMap(values)
)
`))
	qt.Assert(t, qt.Equals(a.GenGet("data"), `genstruct.GetEnum(data, 0, 1, values)`))
	qt.Assert(t, qt.Equals(GetEnum(data, 0, 1, values), "bar"))

	a.Put(data, "baz")
	qt.Assert(t, qt.Equals(GetEnum(data, 0, 1, values), "baz"))
}

func TestEnumMap(t *testing.T) {
	keys := []string{"a", "b", "c", "d"}
	values := []string{"v1", "v2"}
	var s Struct
	a := AddEnumMap(&s, keys, values, "", "m")
	qt.Assert(t, qt.Equals(s.Size(), 1))
	data := make([]byte, s.Size())
	contents := map[string]string{
		"a": "v1",
		"d": "v2",
	}
	a.Put(data, maps.All(contents))
	qt.Assert(t, qt.Equals(data[0], 0b10_00_00_01))
	qt.Assert(t, qt.Equals(a.GenGet("x"), `genstruct.GetEnumMap(x, 0, 1, m_keys, m_values)`))
	got := make(map[string]string)
	maps.Insert(got, GetEnumMap(data, 0, 1, keys, values))
	qt.Assert(t, qt.DeepEquals(got, contents))
}

func TestSet(t *testing.T) {
	values := []string{"foo", "bar", "baz"}
	var s Struct
	a := AddSet(&s, values, "values")
	qt.Assert(t, qt.Equals(s.Size(), 1))
	data := make([]byte, s.Size())
	a.Put(data, slices.Values([]string{"foo", "baz"}))
	qt.Assert(t, qt.Equals(data[0], 0b101))
	a.Put(data, slices.Values([]string{"bar"}))
	qt.Assert(t, qt.Equals(data[0], 0b010))
	qt.Assert(t, qt.Equals(s.GenInit(), `var (
	values = []string {
		"foo",
		"bar",
		"baz",
	}
	values_rev = genstruct.IndexMap(values)
)
`))
	qt.Assert(t, qt.Equals(a.GenGet("data"), `genstruct.GetSet(data, 0, 1, values)`))
	qt.Assert(t, qt.DeepEquals(slices.Collect(GetSet(data, 0, 1, values)), []string{"bar"}))

	a.Put(data, slices.Values([]string{"foo", "baz"}))
	qt.Assert(t, qt.DeepEquals(slices.Collect(GetSet(data, 0, 1, values)), []string{"foo", "baz"}))
}

func TestLargerSet(t *testing.T) {
	values := make([]string, 35)
	for i := range values {
		values[i] = fmt.Sprint(i)
	}
	var s Struct
	a := AddSet(&s, values, "values")
	qt.Assert(t, qt.Equals(s.Size(), 5))
	data := make([]byte, s.Size())
	a.Put(data, slices.Values([]string{"1", "3", "31", "34"}))
	qt.Assert(t, qt.DeepEquals(data, []byte{
		0b00001010,
		0,
		0,
		0b10000000,
		0b00000100,
	}))
	qt.Assert(t, qt.Equals(a.GenGet("data"), `genstruct.GetSet(data, 0, 5, values)`))
	qt.Assert(t, qt.DeepEquals(slices.Collect(GetSet(data, 0, 5, values)), []string{"1", "3", "31", "34"}))
}
