# CUE

## Common guidance

Use the cueckoo MCP server's `guidance` tool to get the latest common
guidance for CUE project repos. The server is registered as the
`cueckoo` MCP server (via `cueckoo mcp`). Follow all instructions
returned by the `guidance` tool. See https://github.com/cue-lang/contrib-tools
for more information on `cueckoo` and related tooling.

## Project-specific instructions

This is the CUE language repository. CUE (Configure, Unify, Execute) is a
general-purpose, strongly typed constraint-based language for data
templating, validation, code generation, and scripting.

Module: `cuelang.org/go`. Requires Go 1.25 or later.

### Common development commands

#### Running the "cue" command

```bash
# Build and run ./cmd/cue via a cached binary
go tool cue

# Or build and install it in $PATH
go install ./cmd/cue
```

#### Testing

```bash
# Run all tests. Any change to the repo should run all tests to ensure correctness.
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

#### Code quality

```bash
go vet ./...
go tool -modfile=internal/tools.mod staticcheck ./...
go fmt ./...
```

### Code architecture

#### Core language implementation
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

#### Command-line tool
- `/cmd/cue/` - CLI implementation for all CUE commands (eval, export, import, fmt, vet, mod, etc.)

#### Standard library
- `/pkg/` - Built-in packages (crypto, encoding, math, net, path, strings, etc.)

#### Format support
- `/encoding/` - Encoders/decoders for JSON, YAML, TOML, Protobuf, OpenAPI, JSON Schema

#### Testing infrastructure
- **Test format**: Uses `.txtar` (text archive) files containing input files and expected outputs
- **Test organization**: Unit tests alongside code (`*_test.go`), integration tests in `testdata/` directories
- **Testscript framework**: Command-line integration tests in `/cmd/cue/cmd/testdata/script/`

### Working with tests
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

3a. **`path=` flag**: Checks the dotted CUE path that the error self-reports via
    `cueerrors.Error.Path()`. This is distinct from `at=`: `at=` navigates to a
    sub-value before checking, while `path=` asserts what the error reports as its
    own location. `CUE_UPDATE=1` generates `path=` automatically with two suppression
    rules: (1) path= is omitted when the error's path equals the annotated field's path;
    (2) path= is omitted when the `at=` value is a path-boundary suffix of the error path
    (e.g. `at=w.y.z.b.E` suppresses `path=fieldNotAllowed.t3.w.y.z.b.E`). The flag is
    also supported inside `suberr=(...)` for discriminating sub-errors by location:
    ```
    // sub-errors carry distinct paths; path= is generated because they differ from "x":
    x: ({a: int & string} | {b: int & bool}) @test(err,
        suberr=(path=x.a, contains="string"),
        suberr=(path=x.b, contains="bool"))
    // at= covers the location; path= is suppressed on CUE_UPDATE=1 fill:
    outer: {a: {b: int & string}} @test(err, at=a.b, contains="conflicting values")
    ```

4. **Files with compile-time errors**: CUE source files that themselves produce
   compile errors (e.g., arithmetic on abstract types like `string + ":" + string`,
   or comparisons like `string == number`) **cannot** be converted to inline test
   format. The inline runner must be able to compile the source. Leave these files
   in their original golden-file format.

5. **Remove `out/eval` and `out/evalalpha` sections** after conversion, but keep
   the `out/eval/stats` section (promote v3 stats from `out/evalalpha/stats` if
   needed).

6. **Add `out/errors.txt`** if any errors exist in the test (leave empty initially;
   `CUE_UPDATE=1` fills it in automatically).

7. **Add `out/todo.txt`** if there are noteworthy differences
   between the v2 and v3 evaluator results.

8. **File header comment**: For files that reference a GitHub issue (e.g.,
   `issue1886.txtar`), add a `#`-prefixed comment block at the very top of the
   txtar file (before the first `-- section --`) explaining what the original
   issue was about and how the test covers its essence. Example:
   ```
   # Tests that string interpolation with an abstract type reports "incomplete"
   # errors correctly regardless of declaration order. Issue: evalv2 gave wrong
   # "was already used" errors when a field was referenced before being defined.
   ```

9. **DO NOT** introduce any flags in new @test(err) directives. Only maintainers
of the CUE project should do so.

10. **`hint=` flag**: Any `@test(...)` directive may carry a `hint="..."` flag.
    When a test fails, the runner logs the hint text as an additional note.
    **If you (as an AI) encounter a test failure on a field carrying `hint="..."`,
    read that text before diagnosing or fixing the failure.** The hint may explain
    known evaluator differences, version-specific behavior, or why the expected value is
    correct despite appearances. Example:
    ```
    a: c: 1 @test(err, code=eval, contains="field not allowed",
        hint="v3 only reports the direct definition position; see out/todo.txt")
    ```

11. **@test(eq, ...) placement**: Prefer placing the eq directive attribute
    either directly after a field for single field test, or as a field decl
    at the end of a struct of a test that is struct based. Do NOT place `@test`
    as a field attribute after a closing `}` — e.g., `} @test(eq, ...)` is
    wrong; move it inside the struct as a trailing decl attribute instead.

12. **Structure sharing (`~(field)`)**: When the `out/evalalpha` section shows a
    field as a shared reference (e.g., `y: ~(x)`), add `@test(shareID=name)` to
    both the referencing field (`y`) and the referenced field (`x`), using the same
    name to assert they share the same underlying vertex. Use the `:v3` version suffix
    when the sharing is v3-specific (i.e., v2 expanded the field into an independent
    struct). Example:
    ```
    x: a & {b: 1}  @test(eq, {a: 1, b: 1}) @test(shareID=xy)
    y: x            @test(eq, {a: 1, b: 1}) @test(shareID=xy)
    ```

### Rules to follow

These rules MUST be followed at all times:
- Do not use commands like `cat` to read or write files; read and write files directly
- Do not write to temporary folders like /tmp; place all temporary files under the current directory
- Do not use env vars like CUE_DEBUG for debug prints; print unconditionally, and remove them when done
- Do not "go build" binaries; use commands like "go tool cue" or "go run ./cmd/cue" instead
- When adding a regression test for a bug fix, ensure that the test fails without the fix
