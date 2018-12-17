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

# The CUE Language Specification

## Introduction

This is a reference manual for the CUE configuration language.
CUE, pronounced cue or Q, is a general-purpose and strongly typed
configuration language.
The CUE tooling, layered on top of CUE, converts this language to
a general purpose scripting language for creating scripts as well as
simple servers.

CUE was designed with cloud configuration, and related systems, in mind,
but is not limited to this domain.
It derives its formalism from relational programming languages.
This formalism allows for managing and reasoning over large amounts of
configuration in a straightforward manner.

The grammar is compact and regular, allowing for easy analysis by automatic
tools such as integrated development environments.

This document is maintained by mpvl@golang.org.
CUE has a lot of similarities with the Go language. This document draws heavily
from the Go specification as a result, Copyright 2009–2018, The Go Authors.

CUE draws its influence from many languages.
Its main influences were BCL/ GCL (internal to Google),
LKB (LinGO), Go, and JSON.
Others are Swift, Javascript, Prolog, NCL (internal to Google), Jsonnet, HCL,
Flabbergast, JSONPath, Haskell, Objective-C, and Python.


## Notation

The syntax is specified using Extended Backus-Naur Form (EBNF):

```
Production  = production_name "=" [ Expression ] "." .
Expression  = Alternative { "|" Alternative } .
Alternative = Term { Term } .
Term        = production_name | token [ "…" token ] | Group | Option | Repetition .
Group       = "(" Expression ")" .
Option      = "[" Expression "]" .
Repetition  = "{" Expression "}" .
```

Productions are expressions constructed from terms and the following operators,
in increasing precedence:

```
|   alternation
()  grouping
[]  option (0 or 1 times)
{}  repetition (0 to n times)
```

Lower-case production names are used to identify lexical tokens. Non-terminals
are in CamelCase. Lexical tokens are enclosed in double quotes "" or back quotes
``.

The form a … b represents the set of characters from a through b as
alternatives. The horizontal ellipsis … is also used elsewhere in the spec to
informally denote various enumerations or code snippets that are not further
specified. The character … (as opposed to the three characters ...) is not a
token of the Go language.


## Source code representation

Source code is Unicode text encoded in UTF-8.
Unless otherwise noted, the text is not canonicalized, so a single
accented code point is distinct from the same character constructed from
combining an accent and a letter; those are treated as two code points.
For simplicity, this document will use the unqualified term character to refer
to a Unicode code point in the source text.

Each code point is distinct; for instance, upper and lower case letters are
different characters.

Implementation restriction: For compatibility with other tools, a compiler may
disallow the NUL character (U+0000) in the source text.

Implementation restriction: For compatibility with other tools, a compiler may
ignore a UTF-8-encoded byte order mark (U+FEFF) if it is the first Unicode code
point in the source text. A byte order mark may be disallowed anywhere else in
the source.


### Characters

The following terms are used to denote specific Unicode character classes:

```
newline        = /* the Unicode code point U+000A */ .
unicode_char   = /* an arbitrary Unicode code point except newline */ .
unicode_letter = /* a Unicode code point classified as "Letter" */ .
unicode_digit  = /* a Unicode code point classified as "Number, decimal digit" */ .
```

In The Unicode Standard 8.0, Section 4.5 "General Category" defines a set of
character categories.
CUE treats all characters in any of the Letter categories Lu, Ll, Lt, Lm, or Lo
as Unicode letters, and those in the Number category Nd as Unicode digits.


### Letters and digits

The underscore character _ (U+005F) is considered a letter.

```
letter        = unicode_letter | "_" .
decimal_digit = "0" … "9" .
octal_digit   = "0" … "7" .
hex_digit     = "0" … "9" | "A" … "F" | "a" … "f" .
```


## Lexical elements

### Comments
Comments serve as program documentation. There are two forms:

1. Line comments start with the character sequence // and stop at the end of the line.
2. General comments start with the character sequence /* and stop with the first subsequent character sequence */.

A comment cannot start inside string literal or inside a comment.
A general comment containing no newlines acts like a space.
Any other comment acts like a newline.


### Tokens

Tokens form the vocabulary of the CUE language. There are four classes:
identifiers, keywords, operators and punctuation, and literals. White space,
formed from spaces (U+0020), horizontal tabs (U+0009), carriage returns
(U+000D), and newlines (U+000A), is ignored except as it separates tokens that
would otherwise combine into a single token. Also, a newline or end of file may
trigger the insertion of a comma. While breaking the input into tokens, the
next token is the longest sequence of characters that form a valid token.


### Commas

The formal grammar uses commas "," as terminators in a number of productions.
CUE programs may omit most of these commas using the following two rule:

When the input is broken into tokens, a comma is automatically inserted into
the token stream immediately after a line's final token if that token is

    - an identifier
    - null, true, false, bottom, an integer, a floating-point, or string literal
    - one of the punctuation ), ], or }


Although commas are automatically inserted, the parser will require
explicit commas between two list elements.

To reflect idiomatic use, examples in this document elide commas using
these rules.


### Identifiers

Identifiers name entities such as fields and aliases.
An identifier is a sequence of one or more letters and digits.
It may not be `_`.
The first character in an identifier must be a letter.

<!--
TODO: allow identifiers as defined in Unicode UAX #31
(https://unicode.org/reports/tr31/).

Identifiers are normalized using the NFC normal form.
-->

```
identifier = letter { letter | unicode_digit } .
```

```
a
_x9
fieldName
αβ
```

<!-- TODO: Allow Unicode identifiers TR 32 http://unicode.org/reports/tr31/ -->

Some identifiers are [predeclared].


### Keywords

CUE has a limited set of keywords.
All keywords may be used as labels (field names).
They cannot, however, be used as identifiers to refer to the same name.


#### Values

The following keywords are values.

```
null         true         false
```

These can never be used to refer to a field of the same name.
This restriction is to ensure compatibility with JSON configuration files.


#### Preamble

The following pseudo keywords are used at the preamble of a CUE file.
After the preamble, they may be used as identifiers to refer to namesake fields.

```
package      import
```


#### Comprehension clauses

The following pseudo keywords are used in comprehensions.

```
for          in           if           let
```

The pseudo keywords `for`, `if` and `let` cannot be used as identifiers to
refer to fields. All others can.

<!--
TODO:
    reduce [to]
    order [by]
-->


#### Arithmetic

The following pseudo keywords can be used as operators in expressions.

```
div          mod          quo          rem
```

These may be used as identifiers to refer to fields in all other contexts.


### Operators and punctuation

The following character sequences represent operators and punctuation:

```
+    &     &&    ==    !=    (    )
-    |     ||    <     <=    [    ]
*    :     !     >     >=    {    }
/    ::    ;     =     ...   ..   .
div  mod   quo   rem   _|_   <-   ,
```
Optional:
```
->   _|_
```


### Integer literals

An integer literal is a sequence of digits representing an integer value.
An optional prefix sets a non-decimal base: 0 for octal,
0x or 0X for hexadecimal, and 0b for binary.
In hexadecimal literals, letters a-f and A-F represent values 10 through 15.
All integers allow intersticial underscores "_";
these have no meaning and are solely for readability.

Decimal integers may have a SI or IEC multiplier.
Multipliers can be used with fractional numbers.
The result of multiplying the fraction with the multiplier is truncated
towards zero if the result is not an integer.

```
int_lit     = decimal_lit | octal_lit | binary_lit | hex_lit .
decimals  = ( "0" … "9" ) { [ "_" ] decimal_digit } .
decimal_lit = ( "1" … "9" ) { [ "_" ] decimal_digit } [ [ "." decimals ] multiplier ] |
            "." decimals multiplier.
octal_lit   = "0" octal_digit { [ "_" ] octal_digit } .
binary_lit  = "0b" binary_digit { binary_digit } .
hex_lit     = "0" ( "x" | "X" ) hex_digit { [ "_" ] hex_digit } .
multiplier  =  "k" | "Ki" | ( "M" | "G" | "T" | "P" | "E" | "Y" | "Z" ) [ "i" ]
```
<!--
TODO: consider 0o766 notation for octal.
--->

```
42
1.5Gi
0600
0xBad_Face
170_141_183_460_469_231_731_687_303_715_884_105_727
```

### Decimal floating-point literals

A decimal floating-point literal is a representation of
a decimal floating-point value.
We will refer to those as floats.
It has an integer part, a decimal point, a fractional part, and an
exponent part.
The integer and fractional part comprise decimal digits; the
exponent part is an `e` or `E` followed by an optionally signed decimal exponent.
One of the integer part or the fractional part may be elided; one of the decimal
point or the exponent may be elided.

```
decimal_lit = decimals "." [ decimals ] [ exponent ] |
            decimals exponent |
            "." decimals [ exponent ] .
exponent  = ( "e" | "E" ) [ "+" | "-" ] decimals .
```

```
0.
72.40
072.40  // == 72.40
2.71828
1.e+0
6.67428e-11
1E6
.25
.12345E+5
```


### String literals
A string literal represents a string constant obtained from concatenating a
sequence of characters. There are three forms: raw string literals and
interpreted strings and bytes sequences.

Raw string literals are character sequences between back quotes, as in
```
`foo`
```
Within the quotes, any character may appear except back quote. The value of a
raw string literal is the string composed of the uninterpreted (implicitly
UTF-8-encoded) characters between the quotes; in particular, backslashes have no
special meaning and the string may contain newlines. Carriage return characters
(`\r`) inside raw string literals are discarded from the raw string value.

Interpreted string and byte sequence literals are character sequences between,
respectively, double and single quotes, as in `"bar"` and `'bar'`.
Within the quotes, any character may appear except newline and,
respectively, unescaped double or single quote.
String literals may only be valid UTF-8.
Byte sequences may contain any sequence of bytes.

Several backslash escapes allow arbitrary values to be encoded as ASCII text
in interpreted strings.
There are four ways to represent the integer value as a numeric constant: `\x`
followed by exactly two hexadecimal digits; \u followed by exactly four
hexadecimal digits; `\U` followed by exactly eight hexadecimal digits, and a
plain backslash `\` followed by exactly three octal digits.
In each case the value of the literal is the value represented by the
digits in the corresponding base.
Hexadecimal and octal escapes are only allowed within byte sequences
(single quotes).

Although these representations all result in an integer, they have different
valid ranges.
Octal escapes must represent a value between 0 and 255 inclusive.
Hexadecimal escapes satisfy this condition by construction.
The escapes `\u` and `\U` represent Unicode code points so within them
some values are illegal, in particular those above `0x10FFFF`.
Surrogate halves are allowed to be compatible with JSON,
but are translated into their non-surrogate equivalent internally.

The three-digit octal (`\nnn`) and two-digit hexadecimal (`\xnn`) escapes
represent individual bytes of the resulting string; all other escapes represent
the (possibly multi-byte) UTF-8 encoding of individual characters.
Thus inside a string literal `\377` and `\xFF` represent a single byte of
value `0xFF=255`, while `ÿ`, `\u00FF`, `\U000000FF` and `\xc3\xbf` represent
the two bytes `0xc3 0xbf` of the UTF-8
encoding of character `U+00FF`.

After a backslash, certain single-character escapes represent special values:

```
\a   U+0007 alert or bell
\b   U+0008 backspace
\f   U+000C form feed
\n   U+000A line feed or newline
\r   U+000D carriage return
\t   U+0009 horizontal tab
\v   U+000b vertical tab
\\   U+005c backslash
\'   U+0027 single quote  (valid escape only within single quoted literals)
\"   U+0022 double quote  (valid escape only within double quoted literals)
```

The escape `\(` is used as an escape for string interpolation.
A `\(` must be followed by a valid CUE Expression, followed by a `)`.

All other sequences starting with a backslash are illegal inside literals.

```
escaped_char     = `\` ( "a" | "b" | "f" | "n" | "r" | "t" | "v" | `\` | "'" | `"` ) .
unicode_value    = unicode_char | little_u_value | big_u_value | escaped_char .
byte_value       = octal_byte_value | hex_byte_value .
octal_byte_value = `\` octal_digit octal_digit octal_digit .
hex_byte_value   = `\` "x" hex_digit hex_digit .
little_u_value   = `\` "u" hex_digit hex_digit hex_digit hex_digit .
big_u_value      = `\` "U" hex_digit hex_digit hex_digit hex_digit
                           hex_digit hex_digit hex_digit hex_digit .

string_lit             = raw_string_lit |
                       interpreted_string_lit |
                       interpreted_bytes_lit |
                       multiline_lit .
interpolation          = "\(" Expression ")" .
raw_string_lit         = "`" { unicode_char | newline } "`" .
interpreted_string_lit = `"` { unicode_value | interpolation } `"` .
interpreted_bytes_lit  = `"` { unicode_value | interpolation | byte_value } `"` .
```

```
`abc`                // same as "abc"
`\n
\n`                  // same as "\\n\n\\n"
'a\000\xab'
'\007'
'\377'
'\xa'        // illegal: too few hexadecimal digits
"\n"
"\""                 // same as `"`
'Hello, world!\n'
"Hello, \( name )!"
"日本語"
"\u65e5本\U00008a9e"
"\xff\u00FF"
"\uD800"             // illegal: surrogate half
"\U00110000"         // illegal: invalid Unicode code point
```

These examples all represent the same string:

```
"日本語"                                 // UTF-8 input text
'日本語'                                 // UTF-8 input text as byte sequence
`日本語`                                 // UTF-8 input text as a raw literal
"\u65e5\u672c\u8a9e"                    // the explicit Unicode code points
"\U000065e5\U0000672c\U00008a9e"        // the explicit Unicode code points
"\xe6\x97\xa5\xe6\x9c\xac\xe8\xaa\x9e"  // the explicit UTF-8 bytes
```

If the source code represents a character as two code points, such as a
combining form involving an accent and a letter, the result will appear as two
code points if placed in a string literal.

Each of the interpreted string variants have a multiline equivalent.
Multiline interpreted strings are like their single-line equivalent,
but allow newline characters.
Carriage return characters (`\r`) inside raw string literals are discarded from
the raw string value.

Multiline interpreted strings and byte sequences respectively start with
a triple double quote (`"""`) or triple single quote (`'''`),
immediately followed by a newline, which is discarded from the string contents.
The string is closed by a matching triple quote, which must be by itself
on a newline, preceded by optional whitespace.
The whitespace before a closing triple quote must appear before any non-empty
line after the opening quote and will be removed from each of these
lines in the string literal.
A closing triple quote may not appear in the string.
To include it is suffices to escape one of the quotes.

```
multiline_lit         = multiline_string_lit | multiline_bytes_lit .
multiline_string_lit  = `"""` newline
                        { unicode_char | interpolation | newline }
                        newline `"""` .
multiline_bytes_lit   = "'''" newline
                        { unicode_char | interpolation | newline | byte_value }
                        newline "'''" .
```

```
"""
    lily:
    out of the water
    out of itself

    bass
    picking bugs
    off the moon
        — Nick Virgilio, Selected Haiku, 1988
    """
```

This represents the same string as:

```
"lily:\nout of the water\nout of itself\n\n" +
"bass\npicking bugs\noff the moon\n" +
"    — Nick Virgilio, Selected Haiku, 1988"
```

<!-- TODO: other values

Support for other values:
- Duration literals
- regualr expessions: `re("[a-z]")`
-->

## Prototypes

CUE defines basic types and structs. The _basic types_ are null, bool,
int, float, string, and bytes.
A _struct_ is a map from a label to a value, where the value may be of any
type.
Lists, provided by CUE as a convenience, are special cases of structs and
are not included in the definition of the type system.

In CUE, all possible types and values are partially ordered in a lattice.
CUE does not distinguish between types and values, only between
concrete and partially defined instances of a certain type.

For example `string` is the identifier used to set of all possible strings.
The string `"hello"` is an instance of such a string and ordered below
this string. The value `42` is not an instance of `string`.


### Ordering and lattices

All possible prototypes are ordered in a lattice,
a partial order where every two elements have a single greatest lower bound.
A value `a` is said to be _greater_ than `b` if `a` orders before `b` in this
partial order.
At the top of this lattice is the single ancestor of all values, called
_top_, denoted `_` in CUE.
At the bottom of this lattice is the value called _bottom_, denoted `_|_`.
A bottom value usually indicates an error.

We say that for any two prototypes `a` and `b` that `a` is an _instance_ of `b`,
denoted `a ⊑ b`, if `b == a` or `b` is more general than `a`
that is if `a` orders before `b` in the partial order
(`⊑` is _not_ a CUE operator).
We also say that `b` subsumes `a` in this case.


An _atom_ is any value whose only instance is itself and bottom. Examples of
atoms are `42`, `"hello"`, `true`, `null`.

A _type_ is any value which is only an instance of itself or top.
This includes `null`: the null value, `bool`: all possible boolean values,
`int`: all integral numbers, `float`, `string`, `bytes`, and `{}`.

A _concrete value_ is any atom or struct with fields of which the values
are itself concrete values, recursively.
A concrete value corresponds to a valid JSON value

A _prototype_ is any concrete value, type, or any instance of a type
that is not a concrete value.
We will informally refer to a prototype as _value_.


```
false ⊑ bool
true  ⊑ bool
true  ⊑ true
5     ⊑ int
bool  ⊑ _
_|_     ⊑ _
_|_     ⊑ _|_

_     ⋢ _|_
_     ⋢ bool
int   ⋢ bool
bool  ⋢ int
false ⋢ true
true  ⋢ false
int   ⋢ 5
5     ⋢ 6
```


### Unification

The _unification_ of values `a` and `b`, denoted as `a & b` in CUE,
is defined as the value `u` such that `u ⊑ a` and `u ⊑ b`,
while for any other value `u'` for which `u' ⊑ a` and `u' ⊑ b`
it holds that `u' ⊑ u`.
The value `u` is also called the _greatest lower bound_ of `a` and `b`.
The greatest lower bound is, given the nature of lattices, always unique.
The unification of `a` with itself is always `a`.
The unification of a value `a` and `b` where `a ⊑ b` is always `a`.

Unification is commutative, transitive, and reflexive.
As a consequence, order of evaluation is irrelevant, a property that is key
to many of the constructs in the CUE language as well as the tooling layered
on top of it.

Syntactically, unification is a [binary expression].


### Disjunction

A _disjunction_ of two values `a` and `b`, denoted as `a | b` in CUE,
is defined as the smallest value `d` such that `a ⊑ d` and `b ⊑ d`.
The disjunction of `a` with itself is always `a`.
The disjunction of a value `a` and `b` where `a ⊑ b` is always `b`.

Syntactically, disjunction is a [binary expression].

Implementations should report an error if for a disjunction `a | ... | b`,
`b` is an instance of `a`, as `b` will be superfluous and can never
be selected as a default.

A value that evaluates to bottom is removed from the disjunction.
A disjunction evaluates to bottom if all of its values evaluate to bottom.

If a disjunction is used in any operation other than unification or another
disjunction, the default value is chosen before operating on it.

```
Expression                Result (without picking default)
(int | string) & "foo"    "foo"
("a" | "b") & "c"         _|_

(3 | 5) + 2               5
```

If the values of a disjunction are unambiguous, its first value may be taken
as a default value.
The default value for a disjunction is selected when:

1. passing it to an argument of a call or index value,
1. using it in any unary or binary expression except for unification or disjunction,
1. using it as the receiver of a call, index, slice, or selector expression, and
1. a value is taken for a configuration.

A value is unambiguous if a disjunction has never been unified with another
disjunction, or if the first element is the result of unifying two first
values of a disjunction.


```
Expression                       Default
("tcp"|"udp") & ("tcp"|"udp")    "tcp"  // default chosen
("tcp"|"udp") & ("udp"|"tcp")    _|_    // no unique default

("a"|"b") & ("b"|"a") & "a"      "a"    // single value after evaluation
```


### Bottom and errors
Any evaluation error in CUE results in a bottom value, respresented by
the token '_|_'.
Bottom is an instance of every other prototype.
Any evaluation error is represented as bottom.

Implementations may associate error strings with different instances of bottom;
logically they remain all the same value.

```
Expr         Result
 1  &  2       _|_
int & bool     _|_
_|_ |  1        1
_|_ &  2       _|_
_|_ & _|_      _|_
```


### Top
Top is represented by the underscore character '_', lexically an identifier.
Unifying any value `v` with top results `v` itself.

```
Expr        Result
_ &  5        5
_ &  _        _
_ & _|_      _|_
_ | _|_       _
```


### Null

The _null value_ is represented with the pseudo keyword `null`.
It has only one parent, top, and one child, bottom.
It is unordered with respect to any other prototype.

```
null_lit   = "null"
```

```
null & 8     _|_
```


### Boolean values

A _boolean type_ represents the set of Boolean truth values denoted by
the pseudo keywords `true` and `false`.
The predeclared boolean type is `bool`; it is a defined type and a separate
element in the lattice.

```
boolean_lit = "true" | "false"
```

```
bool & true       true
bool | true       true
true | false      true | false
true & true       true
true & false      _|_
```


### Numeric values

An _integer type_ represents the set of all integral numbers.
A _decimal floating-point type_ represents of all decimal floating-point
numbers.
They are two distinct types.
The predeclared integer and decimal floating-point type are `int` and `float`;
they are a defined type.

A decimal floating-point literal always has type `float`;
it is not an instance of `int` even if it is an integral number.

An integer literal has both type `int` and `float`, with the integer variant
being the default if no other constraints are applied.
Expressed in terms of disjunction and [type conversion],
the literal `1`, for instance, is defined as `int(1) | float(1)`.

Numeric values are exact values of arbitrary precision and do not overflow.
Consequently, there are no constants denoting the IEEE-754 negative zero,
infinity, and not-a-number values.

Implementation restriction: Although numeric values have arbitrary precision
in the language, implementations may implement them using an internal
representation with limited precision. That said, every implementation must:

Represent integer values with at least 256 bits.
Represent floating-point values, with a mantissa of at least 256 bits and
a signed binary exponent of at least 16 bits.
Give an error if unable to represent an integer value precisely.
Give an error if unable to represent a floating-point value due to overflow.
Round to the nearest representable value if unable to represent
a floating-point value due to limits on precision.
These requirements apply to the result of any expression except builtin
expressions where the loss of precision is remarked.


### Strings

The _string type_ represents the set of all possible UTF-8 strings,
not allowing surrogates.
The predeclared string type is `string`; it is a defined type.

Strings are designed to be unicode-safe.
Comparisson is done using canonical forms ("é" == "e\u0301").
A string element is an extended grapheme cluster, which is an approximation
of a human readable character.
The length of a string is its number of extended grapheme clusters, and can
be discovered using the built-in function `len`.

The length of a string `s` (its size in bytes) can be discovered using
the built-in function len.
A string's extended grapheme cluster can be accessed by integer index
0 through len(s)-1 for any byte that is part of that grapheme cluster.
To access the individual bytes of a string one should convert it to
a sequence of bytes first.


### Ranges

A _range type_, syntactically a [binary expression], defines
a disjunction of concrete values that can be represented as a contiguous range.
Ranges can be defined on numbers and strings.

The type of range `a..b` is the unification of the type of `a` and `b`.
Note that this may be more than one type.

A range of numbers `a..b` defines an inclusive range for integers and
floating-point numbers.

Remember that an integer literal represents both an `int` and `float`:
```
2   & 1..5          // 2,  where 2 is either an int or float.
2.5 & 1..5          // 2.5
2.5 & int & 1..5    // _|_
2.5 & (int & 1)..5  // _|_
2.5 & float & 1..5  // 2.5
0..7 & 3..10        // 3..7
```

A range of strings `a..b` defines a set of strings that includes any `s`
for which `NFC(a) <= NFC(s)` and `NFC(s) <= NFC(b)` in a byte-wise comparison,
where `NFC` is the respective Unicode normal form.


### Structs

A _struct_ is a set of named elements, called _fields_, each of
which has a name, called a _label_, and value.
Structs and fields respectively correspond to JSON objects and members.

We say a label is defined for a struct if the struct has a field with the
corresponding label.
We denote the value for a label `f` defined for `a` as `δ(f, a)`.

A struct `a` is an instance of `b`, or `a ⊑ b`, if for any label `f`
defined for `b` label `f` is also defiend for `a` and `δ(f, a) ⊑ δ(f, b)`.
Note that if `a` is an instance of `b` it may have fields with labels that
are not defined for `b`.

The unification of structs `a` and `b` is defined as a new struct `c` which
has all fields of `a` and `b` where the value is either the unification of the
respective values where a field is contained in both or the original value
for the respective fields of `a` or `b` otherwise.
Any [references] to `a` or `b` in their respective field values need to be
replaced with references to `c`.

Syntactically, a struct literal may contain multiple fields with
the same label, the result of which is a single field with a value
that is the result of unifying the values of those fields.

```
StructLit     = "{" [ { Declaration "," } Declaration ] "}" .
Declaration   = FieldDecl | AliasDecl .
FieldDecl     = Label { Label } ":" Expression .

AliasDecl     = Label "=" Expression .
Label         = identifier | json_string | TemplateLabel | ExprLabel.
TemplateLabel = "<" identifier ">" .
ExprLabel     = "[" Expression "]" .
Tag           = "#" identifier [ ":" json_string ] .
json_string   = `"` { unicode_value } `"`
```

<!--
TODO: consider using string interpolations for ExprLabel, instead of []
So "\(k)" for [k]
--->


```
{a: 1} ⊑ {}
{a: 1, b: 1} ⊑ {a: 1}
{a: 1} ⊑ {a: int}
{a: 1, b: 1} ⊑ {a: int, b: float}

{} ⋢ {a: 1}
{a: 2} ⋢ {a: 1}
{a: 1} ⋢ {b: 1}
```

```
Expression                  Result
{a: int, a: 1}               {a: int(1)}
{a: int} & {a: 1}            {a: 1}
{a: 1..7} & {a: 5..9}        {a: 5..7}
{a: 1..7, a: 5..9}           {a: 5..7}

{a: 1} & {b: 2}              {a: 1, b: 2}
{a: 1, b: int} & {b: 2}      {a: 1, b: 2}

{a: 1} & {a: 2}              _|_
```

A struct literal may, besides fields, also define aliases.
Aliases declare values that can be referred to within the [scope] of their
definition, but are not part of the struct: aliases are irrelevant to
the partial ordering of values and are not emitted as part of any
generated data.
The name of an alias must be unique within the struct literal.

```
// An empty object.
{}

// An object with 3 fields and 1 alias.
{
    alias = 3

    foo: 2
    bar: "a string"

    "not an ident": 4
}
```

A field with as value a struct with a single field may be written as
a sequence of the two field names,
followed by a colon and the value of that single field.

```
job myTask: {
    replicas: 2
}
```
expands to the following JSON:
```
"job": {
    "myTask": {
        "replicas": 2
    }
}
```


A field declaration may be followed by an optional field tag,
which becomes a key value pair for all equivalent fields in structs with which
it is unified.
If two structs are unified which both define a field for a label and both
fields have a tag for that field with the same key,
implementations may require that their value match.
The tags are made visible through CUE's API and are
not visible within the language itself.


### Lists

A list literal defines a new prototype of type list.
A list may be open or closed.
An open list is indicated with a `...` at the end of an element list,
optionally followed by a prototype for the remaining elements.

The length of a closed list is the number of elements it contains.
The length of an open list is the its number of elements as a lower bound
and an unlimited number of elements as its upper bound.

```
ListLit       = "[" [ ElementList [ "," [ "..." [ Element ] ] ] "]" .
ElementList   = Element { "," Element } .
Element       = Expression | LiteralValue .
```
<!---
KeyedElement  = Element .
--->

A list can be represented as a struct:

```
List: null | {
    Elem: _
    Tail: List
}
```

For closed lists, `Tail` is `null` for the last element, for open lists it is
`null | List`.
For instance, the closed list [ 1, 2, ... ] can be represented as:
```
open: List & { Elem: 1, Tail: { Elem: 2 } }
```
and the closed version of this list, [ 1, 2 ], as
```
closed: List & { Elem: 1, Tail: { Elem: 2, Tail: null } }
```

Using this definition, the subsumption and unification rules for lists can
be derived from those of structs.
Implementations are not required to implement lists as structs.


## Declarations and Scopes


### Blocks

A _block_ is a possibly empty sequence of declarations.
A block is mostly corresponds with the brace brackets of a struct literal
`{ ... }`, but also includes the following,

- The _universe block_ encompases all CUE source text.
- Each package has a _package block_ containing all CUE source text in that package.
- Each file has a _file block_ containing all CUE source text in that file.
- Each `for` and `let` clause in a comprehension is considered to be
  its own implicit block.
- Each function value is considered to be its own implicit block.

Blocks nest and influence [scoping].


### Declarations and scope

A _declaration_ binds an identifier to a field, alias, or package.
Every identifier in a program must be declared.
Other than for fields,
no identifier may be declared twice within the same block.
For fields an identifier may be declared more than once within the same block,
resulting in a field with a value that is the result of unifying the values
of all fields with the same identifier.

```
TopLevelDecl   = Declaration | Emit .
Emit           = Operand .
```

The _scope_ of a declared identifier is the extent of source text in which the
identifier denotes the specified field, alias, function, or package.

CUE is lexically scoped using blocks:

1. The scope of a [predeclared identifier] is the universe block.
1. The scope of an identifier denoting a field or alias
  declared at top level (outside any struct literal) is the file block.
1. The scope of the package name of an imported package is the file block of the
  file containing the import declaration.
1. The scope of a field or alias identifier declared inside a struct literal
  is the innermost containing block.

An identifier declared in a block may be redeclared in an inner block.
While the identifier of the inner declaration is in scope, it denotes the entity
declared by the inner declaration.

The package clause is not a declaration;
the package name do not appear in any scope.
Its purpose is to identify the files belonging to the same package
and tospecify the default name for import declarations.


### Predeclared identifiers

```
Functions
len       required  close     open

Types
null      The null type and value
bool      All boolean values
int       All integral numbers
float     All decimal floating-point numbers
string    Any valid UTF-8 sequence
bytes     A blob of bytes representing arbitrary data

Derived   Value
number    int | float
uint      0..int
uint8     0..255
byte      uint8
int8      -128..127
uint16    0..65536
int16     -32_768...32_767
rune      0..0x10FFFF
uint32    0..4_294_967_296
int32     -2_147_483_648..2_147_483_647
uint64    0..18_446_744_073_709_551_615
int64     -9_223_372_036_854_775_808..9_223_372_036_854_775_807
uint128   340_282_366_920_938_463_463_374_607_431_768_211_455

	"int128": mkIntRange(
		"-170141183460469231731687303715884105728",
		"170141183460469231731687303715884105727"),

	// Do not include an alias for "byte", as it would be too easily confused
	// with the builtin "bytes".
	"uint8":   mkIntRange("0", "255"),
	"uint16":  mkIntRange("0", "65535"),
	"uint32":  mkIntRange("0", "4294967295"),
	"uint64":  mkIntRange("0", "18446744073709551615"),
}
```


### Exported and manifested identifiers

An identifier of a package may be exported to permit access to it
from another package.
An identifier is exported if both:
the first character of the identifier's name is not a Unicode lower case letter
(Unicode class "Ll") or the underscore "_"; and
the identifier is declared in the file block.
All other identifiers are not exported.

An identifier that starts with the underscore "_" is not
emitted in any data output.
Quoted labels that start with an underscore are emitted nonetheless.

### Uniqueness of identifiers

Given a set of identifiers, an identifier is called unique if it is different
from every other in the set, after applying normalization following
Unicode Annex #31.
Two identifiers are different if they are spelled differently.
<!--
or if they appear in different packages and are not exported.
--->
Otherwise, they are the same.


### Field declarations

A field declaration binds a label (the names of the field) to an expression.
The name for a quoted string used as label is the string it represents.
Tne name for an identifier used as a label is the identifier itself.
Quoted strings and identifiers can be used used interchangeably, with the
exception of identifiers starting with an underscore '_'.
The latter represent hidden fields and are treated in a different namespace.


### Alias declarations

An alias declaration binds an identifier to the given expression.

Within the scope of the identifier, it serves as an _alias_ for that
expression.
The expression is evaluated in the scope as it was declared.


### Function declarations

NOTE: this is an internal construction.

A function declaration binds an identifier, the function name, to a function.

```
FunctionDecl   = FunctionName Parameters "->" FunctionValue .
FunctionName   = identifier .
FunctionValue  = Expression .
Result         = Parameters .
Parameters     = "(" [ ParameterList [ "," ] ] ")" .
ParameterList  = ParameterDecl { "," ParameterDecl } .
ParameterDecl  = identifier [ ":" Type ] .
```


## Expressions

An expression specifies the computation of a value by applying operators and
functions to operands.


### Operands

Operands denote the elementary values in an expression.
An operand may be a literal, a (possibly [qualified]) identifier denoting
field, alias, function, or a parenthesized expression.

```
Operand     = Literal | OperandName | ListComprehension | "(" Expression ")" .
Literal     = BasicLit | ListLit | StructLit .
BasicLit    = int_lit | float_lit | string_lit |
              null_lit | bool_lit | bottom_lit | top_lit .
OperandName = identifier | QualifiedIdent.
```

### Qualified identifiers

A qualified identifier is an identifier qualified with a package name prefix.

```
QualifiedIdent = PackageName "." identifier .
```

A qualified identifier accesses an identifier in a different package,
which must be [imported].
The identifier must be declared in the [package block] of that package.

```
math.Sin    // denotes the Sin function in package math
```


### Primary expressions

Primary expressions are the operands for unary and binary expressions.

```
PrimaryExpr =
	Operand |
	Conversion |
	PrimaryExpr Selector |
	PrimaryExpr Index |
	PrimaryExpr Slice |
	PrimaryExpr Arguments .

Selector       = "." identifier .
Index          = "[" Expression "]" .
Slice          = "[" [ Expression ] ":" [ Expression ] "]"
Argument       = Expression .
Arguments      = "(" [ ( Argument { "," Argument } ) [ "..." ] [ "," ] ] ")" .
```
<!---
Argument       = Expression | ( identifer ":" Expression ).
--->

```
x
2
(s + ".txt")
f(3.1415, true)
m["foo"]
s[i : j + 1]
obj.color
f.p[i].x
```


### Selectors

For a [primary expression] `x` that is not a [package name],
the selector expression

```
x.f
```

denotes the field `f` of the value `x`.
The identifier `f` is called the field selector.
The type of the selector expression is the type of `f`.
If `x` is a package name, see the section on [qualified identifiers].

Otherwise, if `x` is not a struct, or if `f` does not exist in `x`,
the result of the expression is bottom (an error).

```
T: {
    x: int
    y: 3
}

a: T.x  // int
b: T.y  // 3
c: T.z  // _|_ // field 'z' not found in T
```


### Index expressions

A primary expression of the form

```
a[x]
```

denotes the element of the list, string, or struct `a` indexed by `x`.
The value `x` is called the index or field name, respectively.
The following rules apply:

If `a` is not a struct:

- the index `x` must be of integer type
- the index `x` is in range if `0 <= x < len(a)`, otherwise it is out of range

The result of `a[x]` is

for `a` of list type (including single quoted strings, which are lists of bytes):

- the list element at index `x`, if `x` is within range
- bottom (an error), otherwise

for `a` of string type:

- the grapheme cluster at the `x`th byte (type string), if `x` is within range
- bottom (an error), otherwise

for `a` of struct type:

- the value of the field named `x` of struct `a`, if this field exists
- bottom (an error), otherwise

```
[ 1, 2 ][1]     // 2
[ 1, 2 ][2]     // _|_
"He\u0300?"[0]  // "H"
"He\u0300?"[1]  // "e\u0300"
"He\u0300?"[2]  // "e\u0300"
"He\u0300?"[3]  // "e\u0300"
"He\u0300?"[4]  // "?"
"He\u0300?"[5]  // _|_
```


### Slice expressions

Slice expressions construct a substring or slice from a string or list.

For strings or lists, the primary expression
```
a[low : high]
```
constructs a substring or slice. The indices `low` and `high` select
which elements of operand a appear in the result. The result has indices
starting at 0 and length equal to `high` - `low`.
After slicing the list `a`

```
a := [1, 2, 3, 4, 5]
s := a[1:4]
```
the list s has length 3 and elements
```
s[0] == 2
s[1] == 3
s[2] == 4
```
For convenience, any of the indices may be omitted.
A missing `low` index defaults to zero; a missing `high` index defaults
to the length of the sliced operand:
```
a[2:]  // same as a[2 : len(a)]
a[:3]  // same as a[0 : 3]
a[:]   // same as a[0 : len(a)]
```

Indices are in range if `0 <= low <= high <= len(a)`,
otherwise they are out of range.
For strings, the indices selects the start of the extended grapheme cluster
at byte position indicated by the index.
If any of the slice values is out of range or if `low > high`, the result of
a slice is bottom (error).

```
"He\u0300?"[:2]  // "He\u0300"
"He\u0300?"[1:2] // "e\u0300"
"He\u0300?"[4:5] // "e\u0300?"
```


The result of a successful slice operation is a value of the same type
as the operand.


### Operators

Operators combine operands into expressions.

```
Expression = UnaryExpr | Expression binary_op Expression .
UnaryExpr  = PrimaryExpr | unary_op UnaryExpr .

binary_op  = "|" | "&" | "||" | "&&" | rel_op | add_op | mul_op | ".." .
rel_op     = "==" | "!=" | "<" | "<=" | ">" | ">=" .
add_op     = "+" | "-" .
mul_op     = "*" | "/" | "div" | "mod" | "quo" | "rem" .

unary_op   = "+" | "-" | "!" .
```

Comparisons are discussed [elsewhere]. For other binary operators, the operand
types must be [identical] unless the operation involves untyped [constants]
or durations. For operations involving constants only, see the section on
[constant expressions].

Except for duration operations, if one operand is an untyped [literal] and the
other operand is not, the constant is [converted] to the type of the other
operand.


#### Operator precedence

Unary operators have the highest precedence.

There are eight precedence levels for binary operators.
The `..` operator (range) binds strongest, followed by
multiplication operators, addition operators, comparison operators,
`&&` (logical AND), `||` (logical OR), `&` (unification),
and finally `|` (disjunction):

```
Precedence    Operator
    8             ..
    7             *  /  div mod quo rem
    6             +  -
    5             ==  !=  <  <=  >  >=
    4             &&
    3             ||
    2             &
    1             |
```

Binary operators of the same precedence associate from left to right.
For instance, `x / y * z` is the same as `(x / y) * z`.

```
+x
23 + 3*x[i]
x <= f()
f() || g()
x == y+1 && y == z-1
2 | int
{ a: 1 } & { b: 2 }
```

#### Arithmetic operators

Arithmetic operators apply to numeric values and yield a result of the same type
as the first operand. The three of the four standard arithmetic operators
`(+, -, *)` apply to integer and decimal floating-point types;
`+` and `*` also applies to lists and strings.
`/` only applies to decimal floating-point types and
`div`, `mod`, `quo`, and `rem` only apply to integer types.

```
+    sum                    integers, floats, lists, strings
-    difference             integers, floats
*    product                integers, floats, lists, strings
/    quotient               floats
div  division               integers
mod  modulo                 integers
quo  quotient               integers
rem  remainder              integers
```

#### Integer operators

For two integer values `x` and `y`,
the integer quotient `q = x div y` and remainder `r = x mod y `
satisfy the following relationships:

```
r = x - y*q  with 0 <= r < |y|
```
where `|y|` denotes the absolute value of `y`.

```
 x     y    x div y   x mod y
 5     3       1         2
-5     3      -2         1
 5    -3      -1         2
-5    -3       2         1
```

For two integer values `x` and `y`,
the integer quotient `q = x quo y` and remainder `r = x rem y `
satisfy the following relationships:

```
x = q*y + r  and  |r| < |y|
```

with `x quo y` truncated towards zero.

```
 x     y    x quo y   x rem y
 5     3       1         2
-5     3      -1        -2
 5    -3      -1         2
-5    -3       1        -2
```

A zero divisor in either case results in bottom (an error).

For integer operands, the unary operators `+` and `-` are defined as follows:

```
+x                          is 0 + x
-x    negation              is 0 - x
```


#### Decimal floating-point operators

For decimal floating-point numbers, `+x` is the same as `x`,
while -x is the negation of x.
The result of a floating-point division by zero is bottom (an error).

An implementation may combine multiple floating-point operations into a single
fused operation, possibly across statements, and produce a result that differs
from the value obtained by executing and rounding the instructions individually.


#### List operators

Lists can be concatenated using the `+` operator.
For, list `a` and `b`
```
a + b
```
will produce an open list if `b` is open.
If list `a` is open, only the existing elements will be involved in the
concatenation.

```
[ 1, 2 ]      + [ 3, 4 ]       // [ 1, 2, 3, 4 ]
[ 1, 2, ... ] + [ 3, 4 ]       // [ 1, 2, 3, 4 ]
[ 1, 2 ]      + [ 3, 4, ... ]  // [ 1, 2, 3, 4, ... ]
```

Lists can be multiplied using the `*` operator.
```
3*[1,2]         // [1, 2, 1, 2, 1, 2]

[1, 2, ...int]  // open list of two elements with element type int
4*[byte]        // [byte, byte, byte, byte]
[...byte]       // byte list or arbitrary length
(0..5)*[byte]   // byte list of size 0 through 5

// list with alternating elements of type string and int
uint*[string, int]
```

The following illustrate how typed lists can be encoded as structs:
```
ip:  4*[byte]
ipT: {
    Elem: byte
    Tail: {
        Elem: byte
        Tail: {
            Elem: byte
            Tail: {
                Elem: byte
                Tail: null
            }
        }
    }
}

rangeList:  (1..2)*[int]
rangeListT: null | {
    Elem: int
    Tail: {
        Elem: int
        Tail: null | {
            Elem: int
            Tail: null
        }
    }
}

strIntList:  uint*[string, int]
strIntListT: null | {
    Elem: string
    Tail: {
        Elem: int
        Tail: strIntListT
    }
}
```

#### String operators

Strings can be concatenated using the `+` operator:
```
s := "hi " + name + " and good bye"
```
String addition creates a new string by concatenating the operands.

A string can be repeated by multiplying it:

```
s: "etc. "*3  // "etc. etc. etc. "
```

##### Comparison operators

Comparison operators compare two operands and yield an untyped boolean value.

```
==    equal
!=    not equal
<     less
<=    less or equal
>     greater
>=    greater or equal
```

In any comparison, the types of the two operands must unify.

The equality operators `==` and `!=` apply to operands that are comparable.
The ordering operators `<`, `<=`, `>`, and `>=` apply to operands that are ordered.
These terms and the result of the comparisons are defined as follows:

- Boolean values are comparable.
  Two boolean values are equal if they are either both true or both false.
- Integer values are comparable and ordered, in the usual way.
- Floating-point values are comparable and ordered, as per the definitions
  for binary coded decimals in the IEEE-754-2008 standard.
- String values are comparable and ordered, lexically byte-wise.
- Struct are not comparable.
  Two struct values are equal if their corresponding non-blank fields are equal.
- List are not comparable.
  Two array values are equal if their corresponding elements are equal.
```
c: 3 < 4

x: int
y: int

b3: x == y // b3 has type bool
```

#### Logical operators

Logical operators apply to boolean values and yield a result of the same type
as the operands. The right operand is evaluated conditionally.

```
&&    conditional AND    p && q  is  "if p then q else false"
||    conditional OR     p || q  is  "if p then true else q"
!     NOT                !p      is  "not p"
```


<!--
### TODO TODO TODO

3.14 / 0.0   // illegal: division by zero
Illegal conversions always apply to CUE.

Implementation restriction: A compiler may use rounding while computing untyped floating-point or complex constant expressions; see the implementation restriction in the section on constants. This rounding may cause a floating-point constant expression to be invalid in an integer context, even if it would be integral when calculated using infinite precision, and vice versa.
-->

### Conversions
Conversions are expressions of the form `T(x)` where `T` and `x` are
expressions.
The result is always an instance of `T`.

```
Conversion = Expression "(" Expression [ "," ] ")" .
```

<!---

A literal value `x` can be converted to type T if `x` is representable by a
value of `T`.

As a special case, an integer literal `x` can be converted to a string type
using the same rule as for non-constant x.

Converting a literal yields a typed value as result.

```
uint(iota)               // iota value of type uint
float32(2.718281828)     // 2.718281828 of type float32
complex128(1)            // 1.0 + 0.0i of type complex128
float32(0.49999999)      // 0.5 of type float32
float64(-1e-1000)        // 0.0 of type float64
string('x')              // "x" of type string
string(0x266c)           // "♬" of type string
MyString("foo" + "bar")  // "foobar" of type MyString
string([]byte{'a'})      // not a constant: []byte{'a'} is not a constant
(*int)(nil)              // not a constant: nil is not a constant, *int is not a boolean, numeric, or string type
int(1.2)                 // illegal: 1.2 cannot be represented as an int
string(65.0)             // illegal: 65.0 is not an integer constant
```
--->

A conversion is always allowed if `x` is of the same type as `T` and `x`
is an instance of `T`.

If `T` and `x` of different underlying type, a conversion if
`x` can be converted to a value `x'` of `T`'s type, and
`x'` is an instance of `T`.
A value `x` can be converted to the type of `T` in any of these cases:

- `x` is of type struct and is subsumed by `T` ignoring struct tags.
- `x` and `T` are both integer or floating point types.
- `x` is an integer or a list of bytes or runes and `T` is a string type.
- `x` is a string and `T` is a list of bytes or runes.


[Field tags] are ignored when comparing struct types for identity
for the purpose of conversion:

```
person: {
    name:    string #xml:"Name"
    address: null | {
        street: string #xml:"Street"
        city:   string #xml:"City"
    }  #xml:"Address"
}

person2: {
    name:    string
    address: null | {
        street: string
        city:   string
    }
}

p2 = person(person2)  // ignoring tags, the underlying types are identical
```

Specific rules apply to conversions between numeric types, structs,
or to and from a string type. These conversions may change the representation
of `x`.
All other conversions only change the type but not the representation of x.


#### Conversions between numeric ranges
For the conversion of numeric values, the following rules apply:

1. Any integer prototype can be converted into any other integer prototype
   provided that it is within range.
2. When converting a decimal floating-point number to an integer, the fraction
   is discarded (truncation towards zero). TODO: or disallow truncating?

```
a: uint16(int(1000))  // uint16(1000)
b: uint8(1000)        // _|_ // overflow
c: int(2.5)           // 2  TODO: TBD
```



#### Conversions to and from a string type

Converting a signed or unsigned integer value to a string type yields a string
containing the UTF-8 representation of the integer. Values outside the range of
valid Unicode code points are converted to `"\uFFFD"`.

```
string('a')       // "a"
string(-1)        // "\ufffd" == "\xef\xbf\xbd"
string(0xf8)      // "\u00f8" == "ø" == "\xc3\xb8"

MyString(0x65e5)  // "\u65e5" == "日" == "\xe6\x97\xa5"
```

Converting a list of bytes to a string type yields a string whose successive
bytes are the elements of the slice.
Invalid UTF-8 is converted to `"\uFFFD"`.

```
string('hell\xc3\xb8')   // "hellø"
string(bytes([0x20]))    // " "
```

As string value is always convertible to a list of bytes.

```
bytes("hellø")   // 'hell\xc3\xb8'
bytes("")        // ''
```

<!---
#### Conversions between list types

Conversions between list types are possible only if `T` strictly subsumes `x`
and the result will be the unification of `T` and `x`.

<!---
If we introduce named types this would be different from IP & [10, ...]

Consider removing this until it has a different meaning.

```
IP:        4*[byte]
Private10: IP([10, ...])  // [10, byte, byte, byte]
```
--->

#### Convesions between struct types

A conversion from `x` to `T`
is applied using the following rules:

1. `x` must be an instance of `T`,
2. all fields defined for `x` that are not defined for `T` are removed from
  the result of the conversion, recursively.

```
T: {
    a: { b: 1..10 }
}

x1: {
    a: { b: 8, c: 10 }
    d: 9
}

c1: T(x1)             // { a: { b: 8 } }
c2: T({})             // _|_  // missing field 'a' in '{}'
c3: T({ a: {b: 0} })  // _|_  // field a.b does not unify (0 & 1..10)
```


### Calls

Given an expression `f` of function type F,
```
f(a1, a2, … an)
```
calls `f` with arguments a1, a2, … an. Arguments must be expressions
of which the values are an instance of the parameter types of `F`
and are evaluated before the function is called.

```
a: math.Atan2(x, y)
```
<!---

// FUNCTIONS ARE DISABLED:
Point: {
    Scale(a: float) -> Point({ Value: Point.Value * a })
    Value: float
}
pt: Point
pt: { Value: a }

ptp: pt.Scale(3.5)
--->


In a function call, the function value and arguments are evaluated in the usual
order. After they are evaluated, the parameters of the call are passed by value
to the function and the called function begins execution. The return parameters
of the function are passed by value back to the calling function when the
function returns.

<!---
TODO:

Passing arguments to ... parameters
If f is variadic with a final parameter p of type ...T, then within f the type of p is equivalent to type []T. If f is invoked with no actual arguments for p, the value passed to p is nil. Otherwise, the value passed is a new slice of type []T with a new underlying array whose successive elements are the actual arguments, which all must be assignable to T. The length and capacity of the slice is therefore the number of arguments bound to p and may differ for each call site.

Given the function and calls

func Greeting(prefix string, who ...string)
Greeting("nobody")
Greeting("hello:", "Joe", "Anna", "Eileen")
within Greeting, who will have the value nil in the first call, and []string{"Joe", "Anna", "Eileen"} in the second.

If the final argument is assignable to a slice type []T, it may be passed unchanged as the value for a ...T parameter if the argument is followed by .... In this case no new slice is created.

Given the slice s and call

s := []string{"James", "Jasmine"}
Greeting("goodbye:", s...)
within Greeting, who will have the same value as s with the same underlying array.
--->


### Comprehensions

Lists and fields can be constructed using comprehensions.

Each define a clause sequence that consists of a sequence of `for`, `if`, and
`let` clauses, nesting from left to right.
The `for` and `let` clauses each define a new scope in which new values are
bound to be available for the next clause.

The `for` clause binds the defined identifiers, on each iteration, to the next
value of some iterable value in a new scope.
A `for` clause may bind one or two identifiers.
If there is one identifier, it binds it to the value, for instance
a list element, a struct field value or a range element.
If there are more two identifies, the first value will be the key or index,
if available, and the second will be the value.

An `if` clause, or guard, specifies an expression that terminates the current
iteration if it evaluates to false.

The `let` clause binds the result of an expression to the defined identifier
in a new scope.

A current iteration is said to complete if the inner-most block of the clause
sequence is reached.

List comprehensions specify a single expression that is evaluated and included
in the list for each completed iteration.

Field comprehensions specify a field that is included in the struct for each
completed iteration.
If the same field is generated more than once, its values are unified.
The clauses of a field comprehension may not refer to fields generated by
field comprehensions defined in the same struct.

```
ComprehensionDecl   = Field [ "<-" ] Clauses .
ListComprehension   = "[" Expression "<-" Clauses "]" .

Clauses             = Clause { Clause } .
Clause              = ForClause | GuardClause | LetClause .
ForClause           = "for" identifier [ ", " identifier] "in" Expression .
GuardClause         = "if" Expression .
LetClause           = "let" identifier "=" Expression .
```

```
a: [1, 2, 3, 4]
b: [ x+1 for x in a if x > 1]  // [3, 4, 5]

c: { "\(x)": x + y for x in a if x < 4 let y = 1 }
d: { "1": 2, "2": 3, "3": 4 }
```


### String interpolation

Strings interpolation allows constructing strings by replacing placeholder
expressions included in strings to be replaced with their string representation.
String interpolation may be used in single- and double-quoted strings, as well
as their multiline equivalent.

A placeholder is demarked by a enclosing parentheses prefixed with
a backslash `\`.
Within the parentheses may be any expression to be
evaluated within the scope within which the string is defined.

```
a: "World"
b: "Hello \( a )!" // Hello World!
```


## Builtin Functions

Built-in functions are predeclared. They are called like any other function.

The built-in functions cannot be used as function values.

### `len`

The built-in function `len` takes arguments of various types and return
a result of type int.

```
Argument type    Result

string            string length in bytes
list              list length
struct            number of distinct fields
```


### `required`

The built-in function `required` discards the default mechanism of
a disjunction.

```
"tcp" | "udp"             // default is "tcp"
required("tcp" | "udp")   // no default, user must specify either "tcp" or "udp"
```


## Modules, instances, and packages

CUE configurations are constructed combining _instances_.
An instance, in turn, is constructed from one or more source files belonging
to the same _package_ that together declare the data representation.
Elements of this data representation may be exported and used
in other instances.

### Source file organization

Each source file consists of an optional package clause defining collection
of files to which it belongs,
followed by a possibly empty set of import declarations that declare
packages whose contents it wishes to use, followed by a possibly empty set of
declarations.


```
SourceFile      = [ PackageClause "," ] { ImportDecl "," } { TopLevelDecl "," } .
```

### Package clause

A package clause is an optional clause that defines the package to which
a source file the file belongs.

```
PackageClause  = "package" PackageName .
PackageName    = identifier .
```

The PackageName must not be the blank identifier.

```
package math
```

### Modules and instances
A _module_ defines a tree directories, rooted at the _module root_.

All source files within a module with the same package belong to the same
package.
A module may define multiple package.

An _instance_ of a package is any subset of files within a module belonging
to the same package.
It is interpreted as the concatanation of these files.

An implementation may impose conventions on the layout of package files
to determine which files of a package belongs to an instance.
For instance, an instance may be defined as the subset of package files
belonging to a directory and all its ancestors.


### Import declarations

An import declaration states that the source file containing the declaration
depends on definitions of the _imported_ package (§Program initialization and
execution) and enables access to exported identifiers of that package.
The import names an identifier (PackageName) to be used for access and an
ImportPath that specifies the package to be imported.

```
ImportDecl       = "import" ( ImportSpec | "(" { ImportSpec ";" } ")" ) .
ImportSpec       = [ "." | PackageName ] ImportPath .
ImportPath       = `"` { unicode_value } `"` .
```

The PackageName is used in qualified identifiers to access exported identifiers
of the package within the importing source file.
It is declared in the file block.
If the PackageName is omitted, it defaults to the identifier specified in the
package clause of the imported instance.
If an explicit period (.) appears instead of a name, all the instances's
exported identifiers declared in that instances's package block will be declared
in the importing source file's file block
and must be accessed without a qualifier.

The interpretation of the ImportPath is implementation-dependent but it is
typically either the path of a builtin package or a fully qualifying location
of an instance within a source code repository.

Implementation restriction: An interpreter may restrict ImportPaths to non-empty
strings using only characters belonging to Unicode's L, M, N, P, and S general
categories (the Graphic characters without spaces) and may also exclude the
characters !"#$%&'()*,:;<=>?[\]^`{|} and the Unicode replacement character
U+FFFD.

Assume we have package containing the package clause package math,
which exports function Sin at the path identified by "lib/math".
This table illustrates how Sin is accessed in files
that import the package after the various types of import declaration.

```
Import declaration          Local name of Sin

import   "lib/math"         math.Sin
import m "lib/math"         m.Sin
import . "lib/math"         Sin
```

An import declaration declares a dependency relation between the importing and
imported package. It is illegal for a package to import itself, directly or
indirectly, or to directly import a package without referring to any of its
exported identifiers.


### An example package

TODO

## Interpretation

CUE was inspired by a formalism known as
typed attribute structures [Carpenter 1992] or
typed feature structures [Copestake 2002],
which are used in linguistics to encode grammars and
lexicons. Being able to effectively encode large amounts of data in a rigorous
manner, this formalism seemed like a great fit for large-scale configuration.

Although CUE configurations are specified as trees, not graphs, implementations
can benefit from considering them as graphs when dealing with cycles,
and effectively turning them into graphs when applying techniques like
structure sharing.
Dealing with cycles is well understood for typed attribute structures
and as CUE configurations are formally closely related to them,
we can benefit from this knowledge without reinventing the wheel.


### Formal definition

<!--
The previous section is equivalent to the below text with the main difference
that it is only defined for trees. Technically, structs are more akin dags,
but that is hard to explain at this point and also unnecessarily pedantic.
 We keep the definition closer to trees and will layer treatment
of cycles on top of these definitions to achieve the same result (possibly
without the benefits of structure sharing of a dag).

A _field_ is a field name, or _label_ and a protype.
A _struct_ is a set of _fields_ with unique labels for each field.
-->

A CUE configuration can be defined in terms of constraints, which are
analogous to typed attribute structures referred to above.

#### Definition of basic prototypes

> A _basic prototype_ is any CUE prototype that is not a struct (or, by
> extension, a list).
> All basic prototypes are paritally ordered in a lattice, such that for any
> basic prototype `a` and `b` there is a unique greatest lower bound
> defined for the subsumption relation `a ⊑ b`.

```
Basic prototypes
null
true
bool
3.14
string
"Hello"
0..10
<8
re("Hello .*!")
```

The basic prototypes correspond to their respective types defined earlier.

Struct (and by extension lists), are represented by the abstract notion of
a constraint structure.
Each node in a configuration, including the root node,
is associated with a constraint.


#### Definition of a typed feature structures and substructures

> A typed feature structure_ defined for a finite set of labels `Label`
> is directed acyclic graph with labeled
> arcs and values, represented by a tuple `C = <Q, q0,   υ, δ>`, where
>
> 1. `Q` is the finite set of nodes,
> 1. `q0 ∈ Q`, is the root node,
> 1. `υ: Q → T` is the total node typing function,
>     for a finite set of possible terms `T`.
> 1. `δ: Label × Q → Q` is the partial feature function,
>
> subject to the following conditions:
>
> 1. there is no node `q` or label `l` such that `δ(q, l) = q0` (root)
> 2. for every node `q` in `Q` there is a path `π` (i.e. a sequence of
>    members of Label) such that `δ(q0, π) = q` (unique root, correctness)
> 3. there is no node `q` or path `π` such that `δ(q, π) = q` (no cycles)
>
> where `δ` is extended to be defined on paths as follows:
>
> 1. `δ(q, ϵ) = q`, where `ϵ` is the empty path
> 2. `δ(q, l∙π) = δ(δ(l, q), π)`
>
> The _substructures_ of a typed feature structure are the
> typed feature structures rooted at each node in the structure.
>
> The set of all possible typed feature structures for a given label
> set is denoted as `𝒞`<sub>`Label`</sub>.
>
> The set of _terms_ for label set `Label` is recursively defined as
>
> 1. every basic prototype: `P ⊆ T`
> 2. every constraint in `𝒞`<sub>`Label`</sub> is a term: `𝒞`<sub>`Label`</sub>` ⊆ T`
> 3. for every `n` prototypes `t₁, ..., tₙ`, and every `n`-ary function symbol
>    `f ∈ F_n`, the prototye `f(t₁,...,tₙ) ∈ T`.
>


This definition has been taken and modified from [Carpenter, 1992]
and [Copestake, 2002].

Without loss of generality, we will henceforth assume that the given set
of labels is constant and denote `𝒞`<sub>`Label`</sub> as `𝒞`.

In CUE configurations, the abstract constraints implicated by `υ`
are CUE exressions.
Literal structs can be treated as part of the original typed feature structure
and do not need evaluation.
Any other expression is evaluated and unified with existing values of that node.

References in expressions refer to other nodes within the `C` and represent
a copy of such a `C`.
The functions defined by `F` correspond to the binary and unary operators
and interpolation construct of CUE, as well as builtin functions.

CUE allows duplicate labels within a struct, while the definition of
typed feature structures does not.
A duplicate label `l` with respective values `a` and `b` is represented in
a constraint as a single label with term `&(a, b)`,
the unification of `a` and `b`.
Multiple labels may be recursively combined in any order.

<!-- unnecessary, probably.
#### Definition of evaluated prototype

> A fully evaluated prototype, `T_evaluated ⊆ T` is a subset of `T` consisting
> only of atoms, typed attribute structures and constraint functions.
>
> A prototype is called _ground_ if it is an atom or typed attribute structure.

#### Unification of evaluated prototypes

> A fully evaluated prototype, `T_evaluated ⊆ T` is a subset of `T` consisting
> only of atoms, typed attribute structures and constraint functions.
>
> A prototype is called _ground_ if it is an atom or typed attribute structure.
-->

#### Definition of subsumption and unification on typed attribute structure

> For a given collection of constraints `𝒞`,
> we define `π ≡`<sub>`C`</sub> `π'` to mean that constraint structure `C ∈ 𝒞`
> contains path equivalence between the paths `π` and `π'`
> (i.e. `δ(q0, π) = δ(q0, π')`, where `q0` is the root node of `C`);
> and `𝒫`<sub>`C`</sub>`(π) = c` to mean that
> the constraint structure at the path `π` in `C`
> is `c` (i.e. `𝒫`<sub>`C`</sub>`(π) = c` if and only if `υ(δ(q0, π)) == c`,
> where `q0` is the root node of `C`).
> Subsumption is then defined as follows:
> `C ∈ 𝒞` subsumes `C' ∈ 𝒞`, written `C' ⊑ C`, if and only if:
>
> - `π ≡`<sub>`C`</sub> `π'` implies  `π ≡`<sub>`C'`</sub> `π'`
> - `𝒫`<sub>`C`</sub>`(π) = c` implies`𝒫`<sub>`C'`</sub>`(π) = c` and  `c' ⊑ c`
>
> The unification of `C` and  `C'`, denoted `C ⊓ C'`,
> is the greatest lower bound of `C` and `C'` in `𝒞` ordered by subsumption.

Like with the subsumption relation for basic prototypes,
the subsumption relation for constraints determines the mutual placement
of constraints within the partial order of all values.


#### Evaluation function

> The evaluation function is given by `E: T -> 𝒞`.
> The unification of two constraint structures is evaluated as defined above.
> All other functions are evaluated according to the definitions found earlier
> in this spec.
> An error is indicated by `_|_`.

#### Definition of well-formedness

> We say that a given constraint structure `C = <Q, q0,  υ, δ> ∈ 𝒞` is
> a _well-formed_ constraint structure if and only if for all nodes `q ∈ Q`,
> the substructure `C'` rooted at `q`,
> is such that `E(υ(q)) ∈ 𝒞` and `C' = <Q', q, δ', υ'> ⊑ E(υ(q))`.

<!-- Also, like Copestake, define appropriate features?
Appropriate features are useful for detecting unused variables.

Appropriate features could be introduced by distinguishing between:

a: MyStruct // appropriate features are MyStruct
a: {a : 1}

and

a: MyStruct & { a: 1 } // appropriate features are those of MyStruct + 'a'

This is way to suttle, though.

Alternatively: use Haskell's approach:

a :: MyStruct // define a to be MyStruct any other features are allowed but
              // discarded from the model. Unused features are an error.

Let's first try to see if we can get away with static usage analysis.
A variant would be to define appropriate features unconditionally, but enforce
them only for unused variables, with some looser definition of unused.
-->

The _evaluation_ of a CUE configuration represented by `C`
is defined as the process of making `C` well-formed.

<!--
ore abstractly, we can define this structure as the tuple
`<≡, 𝒫>`, where

- `≡ ⊆ Path × Path` where `π ≡ π'` if and only if `Δ(π, q0) = Δ(π', q0)` (path equivalence)
- `P: Path → ℙ` is `υ(Δ(π, q))` (path value).

A struct `a = <≡, 𝒫>` subsumes a struct `b = <≡', 𝒫'>`, or `a ⊑ b`,
if and only if

- `π ≡ π'` implied `π ≡' π'`, and
- `𝒫(π) = v` implies `𝒫'(π) = v'` and `v' ⊑ v`
-->

#### References
Theory:
- [1992] Bob Carpenter, "The logic of typed feature structures.";
  Cambridge University Press, ISBN:0-521-41932-8
- [2002] Ann Copestake, "Implementing Typed Feature Structure Grammars.";
  CSLI Publications, ISBN 1-57586-261-1

Some graph unification algorithms:

- [1985] Fernando C. N. Pereira, "A structure-sharing representation for
  unification-based grammar formalisms."; In Proc. of the 23rd Annual Meeting of
  the Association for Computational Linguistics. Chicago, IL
- [1991] H. Tomabechi, "Quasi-destructive graph unifications.."; In Proceedings
  of the 29th Annual Meeting of the ACL. Berkeley, CA
- [1992] Hideto Tomabechi, "Quasi-destructive graph ynifications with structure-
   sharing."; In Proceedings of the 15th International Conference on
   Computational Linguistics (COLING-92), Nantes, France.
- [2001] Marcel van Lohuizen, "Memory-efficient and thread-safe
  quasi-destructive graph unification."; In Proceedings of the 38th Meeting of
  the Association for Computational Linguistics. Hong Kong, China.


### Evaluation

The _evaluation_ of a CUE configuration `C` is defined as the process of
making `C` well-formed.

This document does not define any operational semantics.
As the unification operation is communitive, transitive, and reflexive,
implementations have a considerable amount of leeway in
chosing an evaluation strategy.
Although most algorithms for the unification of typed attribute structure
that have been proposed are `O(n)`, there can be considerable performance
benefits of chosing one of the many proposed evaluation strategies over the
other.
Implementations will need to be verified against the above formal definition.



#### Constraint functions

A _constraint function_ is a unary function `f` which for any input `a` only
returns values that are an instance of `a`. For instance, the constraint
function `f` for `string` returns `"foo"` for `f("foo")` and `_|_` for `f(1)`.
Constraint functions may take other constraint functions as arguments to
produce a more restricting constraint function.
For instance, the constraint function `f` for `0..8` returns `5` for `f(5)`,
`5..8` for `f(5..10)`, and `_|_` for `f("foo")`.


Constraint functions play a special role in unification.
The unification function `&(a, b)` is defined as

- `a & b` if `a` and `b` are two atoms
- `a & b` if `a` and `b` are two nodes, respresenting struct
- `a(b)` or `b(a)` if either `a` or `b` is a constraint function, respectively.

Implementations are free to pick which constraint function is applied if
both `a` and `b` are constraint functions, as the properties of unification
will ensure this produces identical results.

#### Manifestation

TODO: a prototype which is a function invocation that cannot be evaluated
or for which the result is not an atom or a struct is called _incomplete_.



### Validation

TODO: when to proactively do recursive validation

#### References

A distinguising feature of CUE's unification algorithm is the use of references.
In conventional graph unification for typed feature structures, the structures
that are unified into the existing graph are independent and pre-evaluated.
In CUE, the constraint structures indicated by references may still need to
be evaluated.
Some conventional evaluation strategy may not cope well with references that
refer to each other.
The simple solution is to deploy a bread-first evaluation strategy, rather than
the more traditional depth-first approach.
Other approaches are possible, however, and implementations are free to choose
which approach is deployed.

### Cycles

TODO: describe precisely which cycles must be resolved by implementations.

Rules:

- Unification of atom value `a` with non-concrete atom `b` for node `q`:
  - set `q` to `a` and schedule the evalution `a == b` at the end of
    evaluating `q`: `C` is only correct under the assumption that `q` is `a`
    so evaluate later.

A direct cyclic reference between nodes defines a shared node for the paths
of the original nodes.

- Unification of cycle of references of struct,
  for instance: `{ a: b, b: c, c: a }`
  - ignore the cycle and continue evaluating not including the last unification:
    a unification of a value with itself is itself. As `a` was already included,
    ignoring the cycle will produce the same result.

```
Configuration    Evaluated
//    c           Cycles in nodes of type struct evaluate
//  ↙︎   ↖         to the fixed point of unifying their.
// a  →  b        values

a: b              // a: { x: 1, y: 3 }  
b: c              // b: { x: 1, y: 3 }  
c: a              // c: { x: 1, y: 3 }

a: { x: 1 }
b: { y: 3 }
```


1. Cycle breaking
1. Cycle detection
1. Assertion checks
1. Validation

The preparation step loads all the relevant CUE sources and merges duplicate
by creating unification expressions until each field is unique within its scope.


For fields of type struct any cycle that does not result in an infinite
structure is allowed.
An expresion of type struct only allows unification and disjunction operations.

Unification of structs is done by unifying a copy of each of the input structs.
A copy of a referenced input struct may itself contain references which are
handled with the following rules:
- a reference bound to a field that it is being copied is replaced
  with a new reference pointing to the respective copy,
- a reference bound to a field that is not being copied refers to the
  original field.


#### Self-referential cycles

A graph unification algorithm like Tomabechi [] or Van Lohuizen [] can be used
to handle the reference replacement rules and minimize the cost of
copying and cycle detection.

Unification of lists, which are expressible as structs, follow along the same
lines.

For an expression `a & b` of any scalar type where exactly one of `a` or `b` is
a concrete value, the result may be replaced by this concrete value while
adding the expression `a == b` to the list of assertions.

```
// Config            Evaluates to
x: {                  x: {
    a: b + 100            a: _|_ // cycle detected
    b: a - 100            b: _|_ // cycle detected
}                     }

y: x & {              y: {
    a: 200                a: 200 // asserted that 200 == b + 100
                          b: 100
}                     }
```

During the evaluation of a field  which expression is being evaluated is marked as such.  

A field `f` with unification expression `e` where `e` contains reference that in turn
point to `a` can be handled as follows:

#### Evaluation cycles

For structs, cycles are disallowed

Disallowed cycles:

A field `a` is _reachable_ from field `b` if there is a selector sequence
from `a` to `b`.

A reference used in field `a` may not refer to a value that recursively
refers to a value that is reachable from `a`.

```
a: b & { c: 3 }

b: a.c  // illegal reference

```

#### Structural cycles

A reference to `Δ(π, q0)` may not recursively refer to `Δ(π', q)`,
where `π` is a prefix to `π'`.


a: b & { b: _ }


### Validation

Implementations are allowed to postpone recursive unification of structures
except for in the following cases:

- Unification within disjunctions:


<!--
### Inference

There is currently no logical inference for values of references prescribed.
It mostly relies on users defining the value of all variables.
The main reason for this is to keep control over run time complexity.
However, implementations may be free to do so.
Also, later versions of the language may strengthen requirements for resolution.

TODO: examples of situations where variables could be resolved but are not.
-->

### Unused values

TODO: rules for detection of unused variables

1. Any alias value must be used

