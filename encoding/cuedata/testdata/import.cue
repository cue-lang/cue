// multiple import statements
import "list"
import "strings"

x: list.MaxItems(3) & [1,2,3]

a: {
	b: strings.ToTitle("foobar")
	c: "barbaz"
}