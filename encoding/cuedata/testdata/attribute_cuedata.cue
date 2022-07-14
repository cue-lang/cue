myStruct1: {
	num:   1
	str:   "foobar"
	$$cue: "// Struct attribute:\n@jsonschema(id=\"https://example.org/mystruct1.json\")\nfield: string\nattr: int"
}
myStruct2: {
	num:   1
	str:   "foobar"
	$$cue: "field: string\nattr: int"
}
num:   1
str:   "foobar"
$$cue: "// Package attribute\n@protobuf(proto3)\nCombined: myStruct1 & myStruct2"
