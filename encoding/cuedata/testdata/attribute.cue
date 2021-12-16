// Package attribute
@protobuf(proto3)

myStruct1: {
    // Struct attribute:
    @jsonschema(id="https://example.org/mystruct1.json")

    // Field attributes
    field: string @go(Field)
    attr:  int    @xml(,attr) @go(Attr)
    num: 1
    str: "foobar"
}

myStruct2: {
		num: 1
    str: "foobar"
    field: string @go(Field)
    attr:  int    @xml(a1,attr) @go(Attr)
}

Combined: myStruct1 & myStruct2
// field: string @go(Field)
// attr:  int    @xml(,attr) @xml(a1,attr) @go(Attr)

num: 1
str: "foobar"