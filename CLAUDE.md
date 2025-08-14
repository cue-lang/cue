# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

This is the CUE language repository. CUE (Configure, Unify, Execute) is a general-purpose, strongly typed constraint-based language for data templating, validation, code generation, and scripting.

## Common Development Commands

### Building
```bash
# Build and install the cue command
go install ./cmd/cue

# Build without installing
go build ./cmd/cue
```

### Testing

```bash
# Run all tests
# Any change to the repo should run all tests to ensure correctness.
go test ./...

# Run tests for a specific package
go test ./internal/core/adt

# Run a specific test function
go test -run TestEvalV2 ./internal/core/adt

# Run a specific testscript test
go test -run TestScript/eval_concrete ./cmd/cue/cmd

# Update golden test files (when output changes are expected)
CUE_UPDATE=1 go test ./...

# Run tests with race detector
go test -race ./...
```

### Code Quality
```bash
# Run go vet (catches common mistakes)
go vet ./...

# Run staticcheck (more comprehensive static analysis)
go tool -modfile=internal/tools.mod staticcheck ./...

# Format code (CUE uses standard Go formatting)
go fmt ./...
```

## Code Architecture

### Core Language Implementation
- `/cue/` - Core CUE language implementation
  - `ast/` - Abstract Syntax Tree
  - `parser/` - Language parser
  - `load/` - Package loading and imports
  - `format/` - Code formatting
- `/internal/core/` - Core evaluation engine
  - `adt/` - Core data structures and algorithms
  - `compile/` - Compilation logic
  - `dep/` - Dependency analysis
  - `export/` - Export functionality

### Command-Line Tool
- `/cmd/cue/` - CLI implementation for all CUE commands (eval, export, import, fmt, vet, mod, etc.)

### Standard Library
- `/pkg/` - Built-in packages (crypto, encoding, math, net, path, strings, etc.)

### Format Support
- `/encoding/` - Encoders/decoders for JSON, YAML, TOML, Protobuf, OpenAPI, JSON Schema

### Testing Infrastructure
- **Test Format**: Uses `.txtar` (text archive) files containing input files and expected outputs
- **Test Organization**: Unit tests alongside code (`*_test.go`), integration tests in `testdata/` directories
- **Testscript Framework**: Command-line integration tests in `/cmd/cue/cmd/testdata/script/`

## Key Development Patterns

### Working with Tests
- Tests use the `.txtar` format which contains both input and expected output in a single file
- Use `TestX` functions in test files for debugging individual test cases
- The `CUE_UPDATE=1` environment variable updates golden files with actual output

### Contribution Model
- Single commit per PR/CL model
- Uses `git codereview` workflow for managing changes
- Runs `cueckoo runtrybot [CL|commit]` to kick off CI testing.
- Requires DCO (Developer Certificate of Origin) sign-off
- Both GitHub PRs and GerritHub CLs are supported
- Changes should be linked to a GitHub issue (except trivial changes)

### Module Information
- Module: `cuelang.org/go`
- Requires Go 1.24 or later
- Uses Go modules for dependency management

### Important Conventions
- Don't update copyright years in existing files
- Follow existing code style and patterns in the package you're modifying
- Check neighboring files for framework choices and conventions
- Use existing libraries and utilities rather than assuming new dependencies
