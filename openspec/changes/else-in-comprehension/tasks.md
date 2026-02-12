## 1. Lexer & Token

- [x] 1.1 Add ELSE token constant to `cue/token/token.go`
- [x] 1.2 Add "else" to keyword map in `cue/token/token.go`

## 2. AST

- [x] 2.1 Add `ElseClause` struct to `cue/ast/ast.go` with Else token.Pos and Body *StructLit fields
- [x] 2.2 Implement `clauseNode()` marker method for `ElseClause`
- [x] 2.3 Implement `Source()` method for `ElseClause`
- [x] 2.4 Add `Else` field to `Comprehension` struct in `cue/ast/ast.go`

## 3. Parser

- [x] 3.1 Extend `parseComprehensionClauses()` in `cue/parser/parser.go` to check for ELSE token after parsing clauses
- [x] 3.2 Parse else clause body as StructLit when ELSE token found
- [x] 3.3 Add error for multiple else clauses
- [x] 3.4 Add parser tests for if...else comprehensions
- [x] 3.5 Add parser tests for for...else comprehensions
- [x] 3.6 Add parser tests for multi-clause comprehensions with else
- [x] 3.7 Add parser error test for multiple else clauses

## 4. Compiler (AST to ADT)

- [x] 4.1 Add `ElseClause` struct to `internal/core/adt/expr.go` with Src and Value fields
- [x] 4.2 Add `Else` field to `Comprehension` struct in `internal/core/adt/expr.go`
- [x] 4.3 Extend `compileClause()` in `internal/core/compile/compile.go` to handle `ast.ElseClause`
- [x] 4.4 Compile else clause body and attach to Comprehension
- [x] 4.5 Add language version check to require v0.16.0 or later for else clause

## 5. Evaluator

- [x] 5.1 Add `yieldCount` field to `compState` in `internal/core/adt/comprehension.go`
- [x] 5.2 Increment `yieldCount` in `compState.yield()` when yielding a value
- [x] 5.3 Modify `OpContext.yield()` to check `yieldCount == 0` after clause evaluation
- [x] 5.4 When `yieldCount == 0` and else clause exists, evaluate and yield else struct
- [x] 5.5 Ensure else clause uses enclosing environment (not comprehension-internal scope)
- [x] 5.6 Fix AST resolver in `cue/ast/astutil/resolve.go` to walk else clause body in outer scope

## 6. Formatter

- [x] 6.1 Update `cue/format/printer.go` to format else clauses in comprehensions

## 7. Tests

- [x] 7.1 Add evaluation tests for if...else in struct comprehensions
- [x] 7.2 Add evaluation tests for for...else with empty source
- [x] 7.3 Add evaluation tests for for...else with filter removing all items
- [x] 7.4 Add evaluation tests for else in list comprehensions
- [x] 7.5 Add evaluation tests for else scoping (outer scope access)
- [x] 7.6 Add evaluation tests for else scoping errors (for/let variable access rejected)
- [x] 7.7 Add evaluation tests for nested comprehensions with else
- [x] 7.8 Add evaluation tests for else not triggering on errors

## 8. Documentation

- [x] 8.1 Update grammar in `doc/ref/spec.md` to include ElseClause
- [x] 8.2 Add comprehension else examples to `doc/ref/spec.md`
- [x] 8.3 Document else clause scoping rules in spec
