# Plan: tolerate replace directives in module.cue (tidy) and omit redundant versions in local-module.cue

## Context

This branch (`rog-314-module-replace`) implements the two-file replace layout:
`cue.mod/module.cue` is the published view, `cue.mod/local-module.cue` holds the
main-module view with replace directives. Today both `cue mod tidy`
(`internal/mod/modload/tidy.go`) and ordinary loading (`cue/load/config.go`)
hard-error if a replace directive appears in `module.cue`.

Two ergonomic relaxations are wanted:

1. **(tidy only)** A replace directive in `module.cue` should no longer be an
   error during `cue mod tidy`. Instead tidy migrates it: it ensures
   `local-module.cue` exists, records the replace there, and removes it from
   `module.cue`. (Per decision: only tidy tolerates this; `cue/load` keeps
   erroring until the module is tidied — so `cue/load/config.go` is left
   unchanged.)

2. **(shared)** In `local-module.cue`, a dep that is also present in
   `module.cue` may omit its `v` (version) field; the version is filled in from
   `module.cue`. Every dependent module must still be *listed* in
   `local-module.cue` — only the version may be dropped when redundant. A dep
   that omits `v` and is **not** in `module.cue` is allowed only if it is a
   replace-only placeholder (has a `replace` and the path carries its major
   version); otherwise it is an error.

## Requirement 2 — omit redundant `v` in local-module.cue (parse level, shared)

This lives in `mod/modfile` and is used by both tidy and `cue/load`.

- **`mod/modfile/schema.cue`**: relax the main-module schema so `v` is optional.
  Change `#File.#Dep` `v!: #Semver | null` → `v?: #Semver | null` (line ~85) and
  update its doc comment to note it may be omitted in `local-module.cue` (filled
  from `module.cue`). `#Strict.#Dep` already declares `v!: #Semver` (line ~124),
  and `#Strict = #File & {...}` unifies optional-with-required to required, so
  published modules still require a version — verify this holds.

- **`mod/modfile/modfile.go` `ParseLocal`**: after `parse(...)` succeeds, walk
  `local.Deps`; for any dep with empty `Version`, fill it from
  `base.Deps[mpath].Version`. If still empty afterwards, allow it only when
  `dep.Replace != ""` (replace-only placeholder; `InitNonStrict`/`module.NewVersion`
  already require the path to carry a major version); otherwise return an error
  like `dependency %q in %s has no version and is not present in module.cue`. Then
  proceed to the existing `eff.InitNonStrict()`.

## Requirement 1 — tolerate & migrate replace in module.cue (`internal/mod/modload/tidy.go`)

- Remove the hard-error loop (current lines 110–114) that rejects replace
  directives in `baseMF`.

- Build a **merged effective local modfile** `effLocalMF` that unions
  `baseMF.Deps` into `localMF.Deps`: local wins on conflict; a `baseMF` dep's
  `replace` is carried over when local has no replace for that path; versions
  come from local when present, else from base. When `localMF == nil` but
  `baseMF` carries replace directives, synthesize `effLocalMF` from `baseMF`
  identity + deps. `Init`/`InitNonStrict` it so `DepVersions`/`DefaultMajorVersions`
  are populated. (Keep this helper local to `tidy.go` since req 1 is tidy-only.)

- Use `effLocalMF` (instead of `localMF`) for: `modpkgload.NewReplacements`,
  seeding `rsLocal` requirements (line ~153), and as the `replSource` passed to
  `modfileFromRequirements` when building the local output (line ~201).

- The published `module.cue` output already strips replaces
  (`modfileFromRequirements(baseMF, rsPub, ..., nil)` rebuilds Deps from resolved
  roots and only reattaches replaces from a non-nil `replSource`). Creating
  `local-module.cue` when replaces exist falls out of the existing
  `hasReplace(local)` gate + `cmd/cue/cmd/modtidy.go` writing `res.Local`.

- **checkTidy**: add an `ErrModuleNotTidy` (e.g. reason "replace directive in
  module.cue should be moved to local-module.cue") when `baseMF` has any replace
  directive, so `cue mod tidy --check` reports not-tidy before migration. Keep
  the existing `localExists && repls == nil` removal check.

## Tests

- **`mod/modfile/modfile_test.go` `TestParseLocal`**: add sub-tests —
  (a) version omitted but module present in base → filled from base;
  (b) version omitted, replace-only placeholder → OK with empty version;
  (c) version omitted, not in base, no replace → error.

- **New `cmd/cue/cmd/testdata/script/modtidy_local_from_module.txtar`**: a
  module with a replace directive in `module.cue`. Assert `cue mod tidy --check`
  reports not-tidy first (this must fail before the fix), then `cue mod tidy`
  moves the replace into `local-module.cue`, removes it from `module.cue`, build
  uses the replacement, and a second `--check` passes (idempotent).

- Add a case (extend `modtidy_local.txtar` or a new file) where
  `local-module.cue` omits `v` for a dep also in `module.cue`, and tidy fills it.

- Confirm regression tests fail without the corresponding fix.

## Verification

```
go test ./mod/modfile/ ./internal/mod/modload/ ./cue/load/
go test -run TestScript/modtidy ./cmd/cue/cmd
go test ./...            # full suite, per project convention
go vet ./... && go fmt ./...
```

Also exercise by hand with `go tool cue mod tidy` in a scratch module whose
`module.cue` contains a `replace`, confirming it migrates to `local-module.cue`.
