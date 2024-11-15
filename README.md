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
[![Documentation](https://img.shields.io/badge/CUE-Docs-0066ff)](https://cuelang.org/docs/)
[![Github](https://github.com/cue-lang/cue/actions/workflows/trybot.yaml/badge.svg)](https://github.com/cue-lang/cue/actions/workflows/trybot.yaml?query=branch%3Amaster+event%3Apush)
[![Go 1.22+](https://img.shields.io/badge/go-1.22-9cf.svg)](https://golang.org/dl/)
[![platforms](https://img.shields.io/badge/platforms-linux|windows|macos-inactive.svg)]()
[![Docker Image](https://img.shields.io/docker/v/cuelang/cue?sort=semver&label=docker)](https://hub.docker.com/r/cuelang/cue)

# CUE - _Configure, Unify, Execute_

CUE makes it easy to validate data, write schemas,
and ensure configurations align with policies.

CUE works with a wide range of tools and formats that you're already using
such as Go, JSON, YAML, OpenAPI, and JSON Schema.

For more information and documentation, including __tutorials and guides__, see [cuelang.org](https://cuelang.org).

### Download and Install

The full range of installation methods for the `cue` command are listed on the
[cuelang.org site](https://cuelang.org/docs/introduction/installation/),
including the official container image suitable for use with Docker.
Here are two common ways to install the command:

#### Release builds

Download the [latest release](https://github.com/cue-lang/cue/releases/latest/) from GitHub.

#### Install from Source

You need [Go 1.22 or later](https://go.dev/doc/install) to install CUE from source:

	go install cuelang.org/go/cmd/cue@latest

You can also clone the repository and build it directly via `go install ./cmd/cue`.
Note that local builds [lack version information](https://go.dev/issue/50603),
so you should inject the version string when building a release, such as:

	git switch -d v0.9.0
	go install -ldflags='-X cuelang.org/go/cmd/cue/cmd.version=v0.9.0' ./cmd/cue

### Learning CUE

The fastest way to learn the basics is to follow the [tour on the website](https://cuelang.org/docs/tour/).

More documentation including various tutorials can be found [on the website](https://cuelang.org/docs/).

### References

- [Language Specification](https://cuelang.org/docs/reference/spec/): the official CUE Language specification
- [Go API](https://pkg.go.dev/cuelang.org/go/cue): the Go API on pkg.go.dev
- [Builtin packages](https://pkg.go.dev/cuelang.org/go/pkg): builtin functions available from CUE programs
- [`cue` CLI](https://cuelang.org/docs/reference/cli/): the `cue` command line interface

### Go release support policy

As a general rule, we support the two most recent major releases of Go,
matching Go's [security policy](https://go.dev/doc/security/policy).
For example, if CUE v0.7.0 is released when Go's latest version is 1.21.5,
v0.7.x including any following bugfix releases will require Go 1.20 or later.

### Contributing

To contribute, please read the [Contribution Guide](CONTRIBUTING.md).

## Code of Conduct

Guidelines for participating in CUE community spaces and a reporting process for
handling issues can be found in the [Code of Conduct](https://cuelang.org/docs/reference/code-of-conduct/).

## Contact

- Ask questions via [GitHub Discussions](https://github.com/cue-lang/cue/discussions)
- Chat with us on [Slack](https://cuelang.org/s/slack) and [Discord](https://cuelang.org/s/discord)
- Subscribe to our [Community Calendar](https://cuelang.org/s/community-calendar) for community updates, demos, office hours, etc

---

Unless otherwise noted, the CUE source files are distributed
under the Apache 2.0 license found in the LICENSE file.

hello
again
cueckoo
