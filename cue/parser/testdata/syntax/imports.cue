// versions: v0.8.0..; outcome: ok
package p

import (
	s "strings"
	"list"
	"encoding/json"
)

import "example.com/foo/bar:baz"

a: s.ToUpper("x")
b: list.Sum([1])
c: json.Marshal({})
d: baz.X
