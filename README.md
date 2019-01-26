<!--
 Copyright 2018 The CUE Authors

 Licensed under the Apache License, Version 2.0 (the "License");
 you may not use this file except in compliance with the License.
 You may obtain a copy of the License at

     http://www.apache.org/licenses/LICENSE-2.0

 Unless required by applicable law or agreed to in writing, software
 distributed under the License is distributed on an "AS IS" BASIS,
 WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 See the License for the specific language governing permissions and
 limitations under the License.
-->


# The CUE Data Constraint Language

_Configure, Unify, Execute_

CUE is an open source data constraint language which aims
to simplify tasks involving defining and using data.
It can be used for data templating, data validation, and even
defining scrips operating on data.

CUE is a constraint-based language.
Constraints act both as data templates and detailed type definitions.
Constraints provide a powerful yet simple alternative
to inheritance, a common source of complexity
with existing configuration languages.
Constraints also provide an expressive way to define the possible
values of data types, which in turn can be used for data validation
in various applications.

The CUE tooling also provides integrated declarative scripting
aimed at simplifying putting configurations to good use while
giving static analysis tools maximum domain knowledge.

Some highlights:

- JSON superset: get started quickly
- convert existing YAML and JSON
- declarative scripting
- automatically simplify configurations
- formatter
- arbitrary-precision arithmetic
- rich APIs designed for automated tooling
- a formalism conducive to automated reasoning
- generate CUE templates from source code
- generate source code from CUE configurations (TODO)


### Download and Install

#### Install From Source

If you already have Go installed, the short version is:

```
go get -u cuelang.org/go/cmd/cue
```

This will install the `cue` command line tool.

For more details see [Installing CUE](./doc/install.md).


### Learning CUE

The fastest way to learn the basics is to follow the
[tutorial on basic language constructs](./doc/tutorial/basics/Readme.md).

A more elaborate tutorial demonstrating of how to convert and restructure
an existing set of Kubernetes configurations is available in
[written form](./doc/tutorial/kubernetes/README.md).

### References

- [Language Specification](./doc/ref/spec.md): official CUE Language specification.

- [API](https://godoc.org/cuelang.org/go/cue): the API on godoc.org

- [Builtin packages](https://godoc.org/cuelang.org/go/pkg): builtins available from CUE programs

- [`cue` Command line reference](./doc/cmd/cue.md): the `cue` command


### Contributing

Our canonical Git repository is located at https://cue.googlesource.com.

To contribute, please read the [Contribution Guide](./doc/contribute.md).

##### Note that we do not accept pull requests and that we use the issue tracker for bug reports and proposals only.

Unless otherwise noted, the CUE source files are distributed
under the Apache 2.0 license found in the LICENSE file.

This is not an officially supported Google product.

