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
[![Go Reference](https://pkg.go.dev/badge/cuelang.org/go.svg)](https://pkg.go.dev/cuelang.org/go)
[![Github](https://github.com/cue-lang/cue/actions/workflows/trybot.yml/badge.svg)](https://github.com/cue-lang/cue/actions/workflows/trybot.yml?query=branch%3Amaster+event%3Apush)
[![Go 1.21+](https://img.shields.io/badge/go-1.21-9cf.svg)](https://golang.org/dl/)
[![platforms](https://img.shields.io/badge/platforms-linux|windows|macos-inactive.svg)]()
[![Docker Image](https://img.shields.io/docker/v/cuelang/cue?sort=semver&label=docker)](https://hub.docker.com/r/cuelang/cue)

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
The same CUE definition can simultaneously be used for validating data
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

#### Release builds

[Download](https://github.com/cue-lang/cue/releases) the latest release from GitHub.

#### Run with Docker

The release binaries are published as a Docker image described by our [Dockerfile](Dockerfile):

	docker run cuelang/cue version

#### Install using Homebrew

Using [Homebrew](https://brew.sh), you can install using the CUE Homebrew tap:

	brew install cue-lang/tap/cue

#### Install from Source

You need Go 1.21 or later to build CUE from source; follow the instructions at https://go.dev/doc/install.

To download and install the `cue` command line tool, run:

	go install cuelang.org/go/cmd/cue@latest

### Learning CUE

The fastest way to learn the basics is to follow the
[tutorial on basic language constructs](https://cuelang.org/docs/tour/).

A more elaborate tutorial demonstrating how to convert and restructure
an existing set of Kubernetes configurations is available in
[written form]( https://github.com/cue-labs/cue-by-example/tree/main/003_kubernetes_tutorial).

### References

- [Language Specification](./doc/ref/spec.md): official CUE Language specification.

- [API](https://pkg.go.dev/cuelang.org/go/cue): the API on pkg.go.dev

- [Builtin packages](https://pkg.go.dev/cuelang.org/go/pkg): builtins available from CUE programs

- [`cue` Command line reference](./doc/cmd/cue.md): the `cue` command

### Go release support policy

As a general rule, we support the two most recent major releases of Go,
matching Go's [security policy](https://go.dev/doc/security/policy).
For example, if CUE v0.7.0 is released when Go's latest version is 1.21.5,
v0.7.x including any following bugfix releases will require Go 1.20 or later.

### Contributing

To contribute, please read the [Contribution Guide](CONTRIBUTING.md).

## Code of Conduct

Guidelines for participating in CUE community spaces and a reporting process for
handling issues can be found in the [Code of
Conduct](https://cuelang.org/docs/contribution_guidelines/conduct).

## Contact

You can get in touch with the cuelang community in the following ways:

- Ask questions via [GitHub Discussions](https://github.com/cue-lang/cue/discussions)
- Chat with us on our [Slack workspace](https://join.slack.com/t/cuelang/shared_invite/enQtNzQwODc3NzYzNTA0LTAxNWQwZGU2YWFiOWFiOWQ4MjVjNGQ2ZTNlMmIxODc4MDVjMDg5YmIyOTMyMjQ2MTkzMTU5ZjA1OGE0OGE1NmE).
- Subscribe to our [Community Calendar](https://cuelang.org/s/community-calendar) for community calls, demos, office hours, etc

---

Unless otherwise noted, the CUE source files are distributed
under the Apache 2.0 license found in the LICENSE file.

