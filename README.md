# The CUE Configuration Language

_Configure, Unify, Execute_

CUE is an open source configuration language which aims
to make complex configurations more manageable and usable.

CUE is a constrained-based language.
Constraints provide a powerful yet simple alternative
to inheritance, a common source of complexity
with other languages.
The CUE tooling also provides powerful integrated scripting
aimed at improving the overall experience of putting
configurations to good use.

Some highlights:

- JSON superset: get started quickly
- convert existing YAML and JSON
- arbitrary-precision arithmetic
- reformatter
- automatically simplify configurations
- powerful scripting
- rich APIs designed for automated tooling
- a formalism conducive to automated reasoning
- generate CUE templates from source code


### Download and Install

#### Install From Source

If you already have Go installed, the short version is:

```
go get -u cuelang.org/go/cmd/cue
```

This will install the `cue` command line tool.

For more details see [Installing CUE][./doc/install.md].


### Learning CUE

A demonstration of how to convert and restructure and existing
set of Kubernetes configurations is available in
[written form][./doc/demo/kubernetes/Readme.md] or as
[video]().

### References

- [Language Specification][./doc/ref/spec.md]: official CUE Language specification.

- [API](https://godoc.org/cuelang.org/go/cue): the API on godoc.org

- [Builtin packages](https://godoc.org/cuelang.org/go/pkg): builtins available from CUE programs

- [`cue` Command line reference][./doc/cmd/cue.md]: the `cue` command


### Contributing

Our canonical Git repository is located at https://cue.googlesource.com.

To contribute, please read the [Contribution Guidelines][./CONTRIBUTING.md]

##### Note that we do not accept pull requests and that we use the issue tracker for bug reports and proposals only.

Unless otherwise noted, the CUE source files are distributed
under the Apache 2.0 license found in the LICENSE file.


