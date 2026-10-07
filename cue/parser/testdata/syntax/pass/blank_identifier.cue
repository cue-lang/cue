import _ "strings"

l: [1]
a: {
	for _, v in l {x: v}
	for k, _ in l {y: k}
}
_: 1
