# Language Features Context

This is the CUE language repository.

## Language Version Gating

- New language features MUST be gated on a minimum language version
- Features should specify the minimum version (e.g., v0.16.0) in the design
- Implementation should check c.experiments.LanguageVersion() and use semver.Compare
- Error message format: "feature X requires language version vX.Y.Z or later; current version is vA.B.C"
- The version check should be in internal/core/compile/compile.go
- See verifyVersion() for the pattern used for builtins

## Experiments

- New language features are often introduced as experiments before becoming stable
- Experiments are defined in internal/cueexperiment/file.go as fields on the File struct
- Each experiment has a tag: `experiment:"preview:vX.Y.Z"` (when introduced) and optionally `stable:vX.Y.Z` (when graduated)
- Users enable experiments via @experiment(name) attribute in CUE files
- Check experiments via c.experiments.FieldName in the compiler
- Consider using experiments for features that may need refinement based on user feedback

## Language Change Checklist

When adding new syntax or AST nodes to the CUE language, the following
packages and files typically need updates. Use this as a comprehensive
checklist when planning tasks:

### 1. Lexer & Token (cue/token/)
- [ ] token.go: Add new token constants
- [ ] token.go: Add keywords to keyword map if applicable

### 2. AST (cue/ast/)
- [ ] ast.go: Add new AST node struct(s)
- [ ] ast.go: Implement marker methods (e.g., clauseNode(), exprNode())
- [ ] ast.go: Add fields to existing structs if extending them
- [ ] walk.go: Add case for new node type in Walk function

### 3. Parser (cue/parser/)
- [ ] parser.go: Add parsing logic for new syntax
- [ ] parser_test.go: Add parser tests

### 4. AST Utilities (cue/ast/astutil/)
- [ ] apply.go: Add case for new node type in Apply function
- [ ] resolve.go: Handle new node in scope resolution if it affects scoping

### 5. AST Debug (internal/astinternal/)
- [ ] debug.go: Add case in DebugStr for new node type

### 6. Compiler - ADT Structures (internal/core/adt/)
- [ ] expr.go: Add ADT struct for new construct
- [ ] expr.go: Add fields to existing ADT structs if extending them

### 7. Compiler - Compilation (internal/core/compile/)
- [ ] compile.go: Add compilation logic for AST to ADT conversion
- [ ] compile.go: Add language version check if feature-gated

### 8. Evaluator (internal/core/adt/)
- [ ] Add evaluation logic in appropriate files (e.g., comprehension.go)

### 9. Debug Output (internal/core/debug/)
- [ ] debug.go: Add case for new ADT node type
- [ ] compact.go: Add case for new ADT node type

### 10. Walker (internal/core/walk/)
- [ ] walk.go: Add case for new ADT node type

### 11. Exporter (internal/core/export/)
- [ ] adt.go: Add export logic for new ADT node to AST conversion

### 12. Dependency Analysis (internal/core/dep/)
- [ ] dep.go: Handle new construct in dependency marking
- [ ] mixed.go: Handle new construct if applicable

### 13. Formatter (cue/format/)
- [ ] node.go: Add formatting logic for new AST node

### 14. Subsumer (internal/core/subsume/)
- [ ] structural.go: Handle new construct if it affects subsumption

### 15. Tests
- [ ] cue/testdata/: Add evaluation tests (.txtar files)
- [ ] cue/parser/parser_test.go: Add parser tests
- [ ] internal/core/dep/testdata/: Add dependency tracking tests
- [ ] internal/core/export/testdata/main/: Add export tests

### 16. Documentation
- [ ] doc/ref/spec.md: Update grammar
- [ ] doc/ref/spec.md: Add examples
- [ ] doc/ref/spec.md: Document semantics and scoping rules

### 17. Experiments (if feature is experimental)
- [ ] internal/cueexperiment/file.go: Add experiment field with tags
- [ ] internal/core/compile/compile.go: Check experiment flag before enabling
- [ ] doc/: Document experiment and how to enable it
- [ ] Consider preview/stable version tags for gradual rollout

### 18. Other Potential Updates
- [ ] cue/load/tags.go: If feature interacts with build tags
- [ ] internal/encoding/: If feature affects encoding/decoding
- [ ] internal/lsp/: If feature needs LSP support
- [ ] tools/fix/fix.go: If feature needs migration tooling
- [ ] tools/trim/: If feature affects trimming
