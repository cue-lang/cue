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


# The CUE Configuration Language

_Configure, Unify, Execute_

CUE is an open source configuration language which aims
to make complex configurations more manageable and usable.

CUE is a constrained-based language.
Constraints provide a powerful yet simple alternative
to inheritance, a common source of complexity
with other configuration languages.

The CUE tooling also provides integrated declarative scripting
aimed at simplifying putting configurations to good use while
giving static analyis tools maximum domain knowledge.

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

A demonstration of how to convert and restructure and existing
set of Kubernetes configurations is available in
[written form](./doc/examples/kubernetes/Readme.md) or as
[video]().

### References

- [Language Specification](./doc/ref/spec.md): official CUE Language specification.

- [API](https://godoc.org/cuelang.org/go/cue): the API on godoc.org

- [Builtin packages](https://godoc.org/cuelang.org/go/pkg): builtins available from CUE programs

- [`cue` Command line reference](./doc/cmd/cue.md): the `cue` command


### Contributing

Our canonical Git repository is located at https://cue.googlesource.com.

To contribute, please read the [Contribution Guidelines](./CONTRIBUTING.md).

##### Note that we do not accept pull requests and that we use the issue tracker for bug reports and proposals only.

Unless otherwise noted, the CUE source files are distributed
under the Apache 2.0 license found in the LICENSE file.


