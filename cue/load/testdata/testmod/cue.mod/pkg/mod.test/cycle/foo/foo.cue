package foo

import "mod.test/cycle/bar"

#Foo1: 1
#Foo2: bar.#Bar1 + 2
#Foo:  bar.#Bar2 + 4
