package pkg1

import (
	"math"
	"strings"
	"cuelang.org/go/encoding/gocode/testdata/pkg2"
)

MyStruct: {
	A:  <=10
	B:  =~"cat" | *"dog"
	O?: OtherStruct
	I:  pkg2.ImportMe
} @go(,complete=Complete)

OtherStruct: {
	A: strings.ContainsAny("X")
	P: pkg2.PickMe
}

String: !="" @go(,validate=ValidateCUE)

SpecialString: =~"special" @go(,type=string)

Omit: int @go(-)

// NonExisting will be omitted as there is no equivalent Go type.
NonExisting: {
	B: string
} @go(-)

// ignore unexported unless explicitly enabled.
foo: int

Ptr: {
	A: math.MultipleOf(10)
}
