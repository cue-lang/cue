# CUE Test Suite

This directory contains a test suite for testing the CUE language. This is only
intended to test language evaluation and exporting. Eventually it will also
contains tests for parsing and formatting. It is not intended to cover
testing of the API itself.

## Overview

### Work in progress

The tests are currently converted from various internal Go tests and the
grouping reflect properties of the current implementation. Once the transition
to the new implementation is completed, tests should be reordered along more
logical lines: such as literals, expressions, references, cycles, etc.


## Forseen Structure

The txtar format allows a collection of files to be defined. Any .cue file
is used as an input. The out/* files, which should not have an extension,
define outputs for various tests. A test definition is active for a certain test
if it contains output for this test.

The comments section of the txtar file may contain additional control inputs for
a test. Each line that starts with a `#` immediately followed by a letter or
digit is specially interpreted. These can be boolean tags (`#foo`) or a
key-value pair (`#key: value`), where the value can be a free-form string.
A line starting with `#` followed by a space is interpreted as a comment.

Lines not starting with a `#` are for interpretation by the testscript package.

This organization allows the same test sets to be used for the testing of
tooling as well as internal libraries.

## Common options

- `#skip`: skip this test case for all tests
- `#skip-{name}`: skip this test for the namesake test
- `#todo-{name}`: skip this test for the namesake test, but run it if the
   `--todo` flag is specified.


## Tests

### cue/internal/compile

Compiles all *.cue files and prints the debug string of the internal
representation. This is not valid CUE.