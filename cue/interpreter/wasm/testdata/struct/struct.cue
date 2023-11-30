@extern("wasm")
package p

import "math"

#vector2: {
	x: float64
	y: float64
}

magnitude2: _ @extern("struct.wasm", abi=c, sig="func(#vector2): float64")
magnitude3: _ @extern("struct.wasm", abi=c, sig="func(#vector3): float64")

_v0: {x: 1, y: 1}
_v1: {x: math.Sqrt2, y: math.Sqrt2}
_v2: {x: 123.456, y: 789.012}

m0: magnitude2(_v0)
m1: magnitude2(_v1)
m2: magnitude2(_v2)

normalize2: _ @extern("struct.wasm", abi=c, sig="func(#vector2): #vector2")
n2: normalize2(_v1)
n2m: magnitude2(n2)

#vector3: {
	x: float64
	y: float64
	z: float64
}

_v3: {x: 1, y: 1, z: 1}
_v4: {x: 0, y: 2, z: 2}
_v5: {x: 3.84900179459750509672765853667971637098401167513417917345734884322651781535288897129144, y: 3.84900179459750509672765853667971637098401167513417917345734884322651781535288897129144, z: 3.84900179459750509672765853667971637098401167513417917345734884322651781535288897129144}

m3: magnitude3(_v3)
m4: magnitude3(_v4)
m5: magnitude3(_v5)

double3: _ @extern("struct.wasm", abi=c, sig="func(#vector3): #vector3")
d4: double3(_v4)

cornucopia: _ @extern("struct.wasm", abi=c, sig="func(#cornucopia): int64")

#cornucopia: {
	b: bool
	n0: int16
	n1: uint8
	n2: int64
}

_c0: {b: true, n0: 10, n1: 20, n2: 30}
_c1: {b: false, n0: 1, n1: 2, n2: 3}
_c2: {b: false, n0: -1, n1: 0, n2: 100}
_c3: {b: false, n0: -15000, n1: 10, n2: -10000000}

c0: cornucopia(_c0)
c1: cornucopia(_c1)
c2: cornucopia(_c2)
c3: cornucopia(_c3)

#foo: {
	b: bool
	bar: #bar
}

#bar: {
	b: bool
	baz: #baz
	n: uint16
}

#baz: {
	vec: #vector2
}

mag: _ @extern("struct.wasm", abi=c, name=magnitude_foo, sig="func(#foo): float64")

mb0: mag({b: false, bar: {b: true, baz: {vec: {x: 1, y: 1}}, n: 0}})
mb1: mag({b: true, bar: {b: false, baz: {vec: {x: 3, y: 4}}, n: 1}})
mb2: mag({b: false, bar: {b: true, baz: {vec: {x: 12, y: 35}}, n: 5}})
mb3: mag({b: false, bar: {b: false, baz: {vec: {x: 3.33, y: 5.55}}, n: 110}})
