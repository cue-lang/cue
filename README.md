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
[![GoDoc](https://godoc.org/cuelang.org/go?status.svg)](https://godoc.org/cuelang.org/go)
[![Appveyor](https://ci.appveyor.com/api/projects/status/0v3ec7p5162hpwpe?svg=true)](https://ci.appveyor.com/project/mpvl/cue)
[![Go Report Card](https://goreportcard.com/badge/github.com/cuelang/cue)](https://goreportcard.com/report/github.com/cuelang/cue)


# The CUE Data Constraint Language

_Configure, Unify, Execute_

CUE is an open source data constraint language which aims
to simplify tasks involving defining and using data.

It is a superset of JSON,
allowing users familiar with JSON to get started quickly.


### What is it for?

You can use CUE to

- define a detailed validation schema for your data (manually or automatically from data)
- reduce boilerplate in your data (manually or automatically from schema)
- extract a schema from code
- generate type definitions and validation code
- merge JSON in a principled way
- define and run declarative scripts


### How?

CUE merges the notion of schema and data.
The same CUE defintion can simultaneously be used for validating data
and act as a template to reduce boilerplate.
Schema definition is enriched with fine-grained value definitions
and default values.
At the same time,
data can be simplified by removing values implied by such detailed definitions.
The merging of these two concepts enables
many tasks to be handled in a principled way.


Constraints provide a simple and well-defined, yet powerful, alternative
to inheritance,
a common source of complexity with configuration languages.


### CUE Scripting

The CUE scripting layer defines declarative scripting, expressed in CUE,
on top of data.
This solves three problems:
working around the closedness of CUE definitions (we say CUE is hermetic),
providing an easy way to share common scripts and workflows for using data,
and giving CUE the knowledge of how data is used to optimize validation.

There are many tools that interpret data or use a specialized language for
a specific domain (Kustomize, Ksonnet).
This solves dealing with data on one level, but the problem it solves may repeat
itself at a higher level when integrating other systems in a workflow.
CUE scripting is generic and allows users to define any workflow.


### Tooling

CUE is designed for automation.
Some aspects of this are:

- convert existing YAML and JSON
- automatically simplify configurations
- rich APIs designed for automated tooling
- formatter
- arbitrary-precision arithmetic
- generate CUE templates from source code
- generate source code from CUE definitions (TODO)


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

To report issues or make a feature request, use the
[issue tracker](https://github.com/cuelang/cue/issues).

Changes can be contributed using Gerrit or Github pull requests.

Unless otherwise noted, the CUE source files are distributed
under the Apache 2.0 license found in the LICENSE file.

This is not an officially supported Google product.

