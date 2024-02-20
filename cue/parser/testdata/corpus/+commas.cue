package foo

import "path/to/pkg"
import name "path/to/pkg"
import . "path/to/pkg"
import      /* ERROR "expected 'STRING', found newline" */
import err  /* ERROR "expected 'STRING', found newline" */

foo: [
	0 // legal JSON
]

bar: [
	0,
	1,
	2,
	3
]
