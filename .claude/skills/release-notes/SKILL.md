---
name: release-notes
description: Draft CUE GitHub release notes from a commit range. Use when writing or assisting with the curated release-note prose for a CUE release — minor, pre-release (alpha/rc), or patch.
---

# Drafting CUE release notes

The release body is the GitHub release text. Write only the curated,
hand-judged prose. The trailing `<details>` "Full list of changes since
vX.Y.Z" block is tool-generated — ignore it entirely.

## Structure

Curated prose = an optional preamble, then `##` sections (optionally
split by `####` subsections).

**Preamble.** If the release has any breaking change, the first line is
verbatim:

> Changes which may break some users are marked below with: :warning:

Omit it otherwise (typical of patch releases).

**Sections**, in this fixed order; omit any that are empty:
`## Language` · `## Evaluator` · `` ## `cmd/cue` `` · `## LSP server` ·
`## Encodings` · `## Standard library` · `## Go API`.
Lead with the headline feature when one dominates, overriding the order
(e.g. the new `cue lsp` led v0.15.0).

**Subsections** (`####`) group a large section or a flagship item:
- Named experiments/features (`` #### The new `try` experiment ``):
  1–3 paragraphs on what it does and how to enable it
  (`@experiment(...)` or a language version), with links to the how-to,
  proposal, and spec CL.
- `#### Performance` / `#### Other changes` split `## Evaluator` when
  there is substantial performance work.
- A `####` may carry `:warning:` when the whole subsection is breaking.

## Entry style

- **One change per paragraph** (blank-line separated, not bullet lists);
  each a complete sentence or two. Closely related changes to one
  flag/command/feature may share a paragraph.
- **Write for CUE users, not evaluator authors.** Describe the observable
  effect, not the implementation. Avoid internals ("arcs/vertices",
  "scope chain", "pushdown", "materializing fields"); name the symptom
  ("failed as an incomplete value or a cycle", "relative references now
  resolve correctly"). Internal/roadmap terms (`evalv4`, "comparing to
  bottom") may appear only as forward-looking context.
- **Tense/voice**: descriptive present or imperative — "The new `--chdir`
  flag …", "`cue import --path` now skips …", "Fix a panic which could
  occur when …".
- **Backticks** for anything code-like: flags (`--outfile`), commands
  (`cue mod publish`), identifiers, keywords, attributes (`@embed`), env
  vars (`$DOCKER_AUTH_CONFIG`), types. Name encodings plainly in prose
  ("the JSON Schema encoder", "the ProtoBuf decoder"); use the import
  path (`encoding/jsonschema`) only for a specific API symbol such as
  `GenerateConfig.NameFunc`.
- **Breaking changes**: prefix `:warning:`, and phrase so the impact and
  migration path are clear.
- **Regressions** name the version that introduced them ("a regression
  introduced in `v0.12.0`"); plain bugs need not.
- **Quantify performance** ("up to 80% faster", "memory down by as much
  as 60%"); credit the Unity service where relevant.
- **Aggregate** many small same-theme fixes into one paragraph, often
  closing with gratitude: "A number of panics and other bugs … have been
  fixed; thank you to all who reported these."
- **Reminders**: ongoing multi-release experiments may get a one-line
  note at the top of `## Language`.

### Links

- Go API: pkg.go.dev pinned to the tag — e.g.
  `https://pkg.go.dev/cuelang.org/go/pkg/net@v0.16.0#InCIDR`.
- Issues `https://cuelang.org/issue/NNN` · CLs `https://cuelang.org/cl/NNN`
  · Discussions/proposals `https://cuelang.org/discussion/NNN` · How-tos
  `https://cuelang.org/docs/howto/...`.
- LSP sections link the Getting Started wiki and invite bug reports via
  the issue tracker and the `#lsp` Discord/Slack channels.

## What to include

A 300+-commit release yields ~20–40 prose entries. The test for every
candidate is: **would a user notice or care?** Membership in an Include
category is necessary, not sufficient — drop or aggregate a change whose
audience is narrow or whose effect is cosmetic (a reworded error, a
usage-line tweak, a doc fix). A lean, high-signal section beats an
exhaustive list.

When a change fixes a GitHub issue, the issue's engagement signals how
many users are affected: two or more 👍 reactions, or four or more
unique commenters, indicates the change matters to multiple users and is
worth including.

**Include:**
- New language features, syntax, experiments (and experiments going
  stable, renamed, or reworked).
- New or changed CLI flags, commands, and behaviors.
- New stdlib functions/packages; new or changed public Go API
  (`cue`, `cue/ast`, `cue/load`, …), including deprecations.
- New encoding support and encoder/decoder fixes (JSON Schema, YAML,
  TOML, Protobuf, `cue get go`).
- Bug fixes users hit — especially panics and regressions.
- **Major evaluator changes/refactors — summarize in simple,
  user-facing terms and explain *why* (the bugs it resolves,
  the future work it enables, or if it helps performance).
- Performance/memory improvements, quantified.
- LSP features and notable fixes.
- **Breaking changes and removals — always, with `:warning:`**, including
  removal of long-deprecated APIs.

**Exclude**: ordinary internal refactors and cleanup
(major evaluator reworks excepted, above); test, test-framework, and
regression-test commits; CI, build, tooling, and dependency bumps;
doc-only and comment fixes; anything with no observable effect on the
CLI, the language, or the Go API.

## Release types

- **Minor** (`vX.Y.0`): full treatment — `## Language` with experiment
  subsections, performance write-ups, every applicable section; reference
  the previous minor as the diff base.
- **Pre-release** (`-alpha.N` / `-rc.N`): same structure as the minor it
  leads to; content accumulates into the final `.0`. RCs often document
  late design tweaks under a `:warning:` subsection.
- **Patch** (`vX.Y.Z`, Z>0): short and fix-focused — usually no
  `## Language` section, no warning legend; phrase entries as "Fix a …".
