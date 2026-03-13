# Schema-Guided XML: Gap Analysis

This document captures known gaps in the `encoding/xml/schema` package,
limited to the data-only encoding (we are not considering representation of CUE
schema in XML).

## XML features not yet covered

### Namespaces

The plumbing exists (`@xml(ns=<prefix>)` and `WithNamespace`), but there are no
tests, and several aspects are missing:

- Default namespace declarations (`xmlns="..."`) are not detected or generated.
- Namespace inheritance (a child inheriting the namespace of its parent) is not
  modelled.
- The encoder sets `name.Space` on attributes but does not emit `xmlns:prefix`
  declarations on the root element — Go's `xml.Encoder` may or may not add
  them depending on usage.
- There is no way to express the namespace of the root element itself.

### Root element control

The encoder's root tag is a plain string (`"root"` when called from `cmd/cue`).
There is no way to:

- Control the root element's namespace.
- Attach attributes to the root element from the schema (attributes are encoded
  from the schema mapping, so this does work for fields inside the root, but
  the root tag name itself is a separate parameter with no namespace or
  attribute support beyond what the schema provides).

### Mixed content

XML allows interleaved text and child elements:

```xml
<p>Hello <b>world</b> and goodbye</p>
```

The decoder accumulates all `CharData` into a single `strings.Builder` and only
surfaces it via `@xml(body)` at the end. The ordering of text segments relative
to child elements is lost. The encoder similarly writes the body text first,
then all children.

### Comments, processing instructions, CDATA

- XML comments (`<!-- ... -->`) are silently discarded by the decoder and cannot
  be produced by the encoder.
- Processing instructions (`<?target data?>`) are similarly ignored.
- CDATA sections are transparently converted to `CharData` by Go's
  `xml.Decoder`, so they decode fine, but the encoder has no way to force CDATA
  output.

### XML declaration

The encoder does not emit an XML declaration (`<?xml version="1.0" ...?>`).
This is often expected in standalone XML documents.

### Empty vs. absent elements

An optional CUE field that is absent produces no XML element on encode.
However, there is no way to distinguish between an empty element (`<foo/>`) and
an element with empty text content (`<foo></foo>`) — both decode to the same
CUE value. This is unlikely to matter in practice since XML treats them
identically.

### Attribute ordering

`sortedAttrs` iterates a Go map, so attribute order in the encoded XML is
non-deterministic. XML does not ascribe significance to attribute order, but
deterministic output is useful for testing and diffing.

## CUE types not yet covered

### Bytes

There is no mapping for `bytes` values. A natural encoding would be base64 text
content, but this is not implemented. The decoder falls through to the default
string case; the encoder returns an error.

### Null / bottom

`null` has no XML representation. The encoder skips non-concrete values, which
is reasonable, but the decoder has no way to produce a `null` — an absent
element is simply omitted from the output.

### Disjunctions

A schema field like `field?: "a" | "b" | "c"` works for decoding (the
`IncompleteKind` is string, so it decodes as a string). However, the
interaction is implicit — the schema constraint is not used for validation
during decode, so an invalid value like `"d"` would still be accepted. On the
encode side, the concrete value is used directly, so disjunctions work as long
as the value is concrete.

### Large integers

The decoder handles arbitrary-precision integers via `literal.ParseNum`, but
the encoder uses `v.Int64()`, which fails for values outside the int64 range.
This asymmetry means a round-trip can fail for large integers.

### Bytes-like types (base64, hex)

No special handling for fields that should decode from/to base64 or
hex-encoded XML text. A schema annotation (e.g. `@xml(encoding=base64)`) could
support this in future.

## Schema features not leveraged

### Constraints used only for structure, not validation

The schema's type constraints (e.g. `>=0`, `=~"^[a-z]+$"`, `<100`) are not
checked during decode. The schema is used purely for structural guidance (which
fields exist, what kinds they are, how they map to XML). Validation is expected
to happen separately by unifying the decoded CUE with the schema.

This is arguably the right design — it separates concerns — but it means the
decoder can produce values that don't satisfy the schema.

### Default values

The schema may define default values (`field: *"default" | string`), but the
decoder does not apply defaults for absent XML fields. Defaults are expected to
be resolved by CUE's own unification after decoding.

### Definitions and hidden fields

Only regular (possibly optional) fields are considered by the schema parser
(`schema.Fields(cue.Optional(true))`). Definitions (`#Foo`) and hidden fields
(`_foo`) in the schema are ignored.

## Testing gaps

- No namespace tests (despite code support).
- No error-case tests (invalid XML, schema mismatches, type conversion
  failures).
- No tests for deeply nested structures or recursive schemas.
- No tests for Unicode content or special characters beyond the apostrophe in
  the simple encode test.
- No tests for large numeric values.
- No tests for the `number` CUE kind (as opposed to `int` or `float`).
- No round-trip tests (decode then re-encode, verifying XML equivalence).
