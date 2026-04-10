# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

This is the CUE language repository. CUE (Configure, Unify, Execute) is a general-purpose, strongly typed constraint-based language for data templating, validation, code generation, and scripting.

## Common Development Commands

### Running the "cue" command

```bash
# Build and run ./cmd/cue via a cached binary
go tool cue

# Or build and install it in $PATH.
go install ./cmd/cue
```

### Testing

```bash
# Run all tests
# Any change to the repo should run all tests to ensure correctness.
go test ./...

# Run tests for a specific package
go test ./internal/core/adt

# Run a specific test function
go test -run TestEvalV3 ./internal/core/adt

# Run a specific sub-test
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
- Prefer adding test cases to an existing txtar file that is appropriate for the type of reproducer

### Converting txtar tests to inline `@test(...)` format

When converting a txtar test from golden-file format to inline annotations:

1. **Section ordering**: The `-- out/errors.txt --` section must directly follow the
   last input section (e.g., `-- in.cue --`), before `-- out/compile --` and
   `-- out/eval/stats --`.

2. **Error annotations**: Always include position information using a `pos=[]`
   placeholder; `CUE_UPDATE=1` fills it in automatically. Positions are matched
   order-independently; commas between specs are optional. Example:
   ```
   x: bad @test(err, code=eval, contains="...", pos=[])
   ```

3. **Multiple sub-errors**: When an error has multiple sub-errors (e.g., from a
   failed disjunction), use `suberr=(...)` to match individual sub-errors instead
   of a single flat `contains=`:
   ```
   x: a | b @test(err, suberr=(code=eval, contains="..."), suberr=(code=eval, contains="..."))
   ```

4. **Files with compile-time errors**: CUE source files that themselves produce
   compile errors (e.g., arithmetic on abstract types like `string + ":" + string`,
   or comparisons like `string == number`) **cannot** be converted to inline test
   format. The inline runner must be able to compile the source. Leave these files
   in their original golden-file format.

5. **Definition references in `@test(leq, ...)`**: Definition names (e.g.,
   `#MyDef`) are not in scope when the expected constraint expression is compiled.
   Use the structural equivalent (e.g., `{field: string}`) with `@test(closed)`.

6. **Remove `out/eval` and `out/evalalpha` sections** after conversion, but keep
   the `out/eval/stats` section (promote v3 stats from `out/evalalpha/stats` if
   needed).

7. **Add `out/errors.txt`** if any errors exist in the test (leave empty initially;
   `CUE_UPDATE=1` fills it in automatically).

8. **Add `out/todo.txt`** if there are noteworthy differences
   between the v2 and v3 evaluator results.

9. **File header comment**: For files that reference a GitHub issue (e.g.,
   `issue1886.txtar`), add a `#`-prefixed comment block at the very top of the
   txtar file (before the first `-- section --`) explaining what the original
   issue was about and how the test covers its essence. Example:
   ```
   # Tests that string interpolation with an abstract type reports "incomplete"
   # errors correctly regardless of declaration order. Issue: evalv2 gave wrong
   # "was already used" errors when a field was referenced before being defined.
   ```

10. **DO NOT** introduce any flags in new @test(err) directives. Only maintainers
of the CUE project should do so.

11. **`hint=` flag**: Any `@test(...)` directive may carry a `hint="..."` flag.
    When a test fails, the runner logs the hint text as an additional note.
    **If you (as an AI) encounter a test failure on a field carrying `hint="..."`,
    read that text before diagnosing or fixing the failure.** The hint may explain
    known evaluator differences, version-specific behavior, or why the expected value is
    correct despite appearances. Example:
    ```
    a: c: 1 @test(err, code=eval, contains="field not allowed",
        hint="v3 only reports the direct definition position; see out/todo.txt")
    ```

12. **@test(eq, ...) placement**: Prefer placing the eq directive attribute
    either directly after a field for single field test, or as a field decl
    at the end of a struct of a test that is struct based. Do NOT place `@test`
    as a field attribute after a closing `}` — e.g., `} @test(eq, ...)` is
    wrong; move it inside the struct as a trailing decl attribute instead.

13. **Structure sharing (`~(field)`)**: When the `out/evalalpha` section shows a
    field as a shared reference (e.g., `y: ~(x)`), add `@test(shareID=name)` to
    both the referencing field (`y`) and the referenced field (`x`), using the same
    name to assert they share the same underlying vertex. Use the `:v3` version suffix
    when the sharing is v3-specific (i.e., v2 expanded the field into an independent
    struct). Example:
    ```
    x: a & {b: 1}  @test(eq, {a: 1, b: 1}) @test(shareID=xy)
    y: x            @test(eq, {a: 1, b: 1}) @test(shareID=xy)
    ```

### Contribution Model
- Single commit per PR/CL model
- Uses `git codereview` workflow for managing changes
- Runs `cueckoo runtrybot [CL|commit]` to kick off CI testing.
- Requires DCO (Developer Certificate of Origin) sign-off
- Both GitHub PRs and GerritHub CLs are supported
- Changes should be linked to a GitHub issue (except trivial changes)

### Module Information
- Module: `cuelang.org/go`
- Requires Go 1.25 or later
- Uses Go modules for dependency management

### Important Conventions
- Don't update copyright years in existing files
- Follow existing code style and patterns in the package you're modifying
- Check neighboring files for framework choices and conventions
- Use existing libraries and utilities rather than assuming new dependencies

## Rules to follow

These rules MUST be followed at all times:
- Do not use commands like `cat` to read or write files; read and write files directly
- Do not write to temporary folders like /tmp; place all temporary files under the current directory
- Do not use env vars like CUE_DEBUG for debug prints; print unconditionally, and remove them when done
- Do not "go build" binaries; use commands like "go tool cue" or "go run ./cmd/cue" instead
- When adding a regression test for a bug fix, ensure that the test fails without the fix
