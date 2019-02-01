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

This is a reference manual for the CUE data constraint language.
CUE, pronounced cue or Q, is a general-purpose and strongly typed
constraint-based language.
It can be used for data templating, data validation, code generation, scripting,
and many other applications involving structured data.
The CUE tooling, layered on top of CUE, provides
a general purpose scripting language for creating scripts as well as
simple servers, also expressed in CUE.

CUE was designed with cloud configuration, and related systems, in mind,
but is not limited to this domain.
It derives its formalism from relational programming languages.
This formalism allows for managing and reasoning over large amounts of
data in a straightforward manner.

The grammar is compact and regular, allowing for easy analysis by automatic
tools such as integrated development environments.

This document is maintained by mpvl@golang.org.
CUE has a lot of similarities with the Go language. This document draws heavily
from the Go specification as a result.

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
CUE programs may omit most of these commas using the following two rules:

When the input is broken into tokens, a comma is automatically inserted into
the token stream immediately after a line's final token if that token is

- an identifier
- null, true, false, bottom, or an integer, floating-point, or string literal
- one of the characters ), ], or }


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

Some identifiers are [predeclared](#predeclared-identifiers).


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

The following keywords are used at the preamble of a CUE file.
After the preamble, they may be used as identifiers to refer to namesake fields.

```
package      import
```


#### Comprehension clauses

The following keywords are used in comprehensions.

```
for          in           if           let
```

The keywords `for`, `if` and `let` cannot be used as identifiers to
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
+    div   &&    ==    !=    (    )
-    mod   ||    <     <=    [    ]
*    quo   !     >     >=    {    }
/    rem   &     :     <-    ;    ,
%    _|_   |     =     ...   ..   .
```
<!-- :: for "is-a" definitions -->

### Integer literals

An integer literal is a sequence of digits representing an integer value.
An optional prefix sets a non-decimal base: 0 for octal,
0x or 0X for hexadecimal, and 0b for binary.
In hexadecimal literals, letters a-f and A-F represent values 10 through 15.
All integers allow interstitial underscores "_";
these have no meaning and are solely for readability.

Decimal integers may have a SI or IEC multiplier.
Multipliers can be used with fractional numbers.
When multiplying a fraction by a multiplier, the result is truncated
towards zero if it is not an integer.

```
int_lit     = decimal_lit | octal_lit | binary_lit | hex_lit .
decimals  = ( "0" … "9" ) { [ "_" ] decimal_digit } .
decimal_lit = ( "1" … "9" ) { [ "_" ] decimal_digit } [ [ "." decimals ] multiplier ] |
            "." decimals multiplier.
binary_lit  = "0b" binary_digit { binary_digit } .
hex_lit     = "0" ( "x" | "X" ) hex_digit { [ "_" ] hex_digit } .
octal_lit   = "0o" octal_digit { [ "_" ] octal_digit } .
multiplier  = ( "K" | "M" | "G" | "T" | "P" | "E" | "Y" | "Z" ) [ "i" ]
```

```
42
1.5Gi
170_141_183_460_469_231_731_687_303_715_884_105_727
0xBad_Face
0o755
0b0101_0001
```

### Decimal floating-point literals

A decimal floating-point literal is a representation of
a decimal floating-point value (a _float_).
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


### String and byte sequence literals

A string literal represents a string constant obtained from concatenating a
sequence of characters.
Byte sequences are a sequence of bytes.

String and byte sequence literals are character sequences between,
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
\/   U+002f slash (solidus)
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

string_lit             = interpreted_string_lit |
                         interpreted_bytes_lit |
                         multiline_lit .

interpolation          = "\(" Expression ")" .
interpreted_string_lit = `"` { unicode_value | interpolation } `"` .
interpreted_bytes_lit  = `"` { unicode_value | interpolation | byte_value } `"` .
```

```
'a\000\xab'
'\007'
'\377'
'\xa'        // illegal: too few hexadecimal digits
"\n"
"\""
'Hello, world!\n'
"Hello, \( name )!"
"日本語"
"\u65e5本\U00008a9e"
"\xff\u00FF"
"\uD800"             // illegal: surrogate half (TODO: probably should allow)
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
- regular expessions: `re("[a-z]")`
-->


## Values

In addition to simple values like `"hello"` and `42.0`, CUE has _structs_.
A struct is a map from labels to values, like `{a: 42.0, b: "hello"}`.
Structs are CUE's only way of building up complex values;
lists, which we will see later,
are defined in terms of structs.

All possible values are ordered in a lattice,
a partial order where every two elements have a single greatest lower bound.
A value `a` is an _instance_ of a value `b`,
denoted `a ⊑ b`, if `b == a` or `b` is more general than `a`,
that is if `a` orders before `b` in the partial order
(`⊑` is _not_ a CUE operator).
We also say that `b` _subsumes_ `a` in this case.
In graphical terms, `b` is "above" `a` in the lattice.

At the top of the lattice is the single ancestor of all values, called
_top_, denoted `_` in CUE.
Every value is an instance of top.

At the bottom of the lattice is the value called _bottom_, denoted `_|_`.
A bottom value usually indicates an error.
Bottom is an instance of every value.

An _atom_ is any value whose only instances are itself and bottom.
Examples of atoms are `42.0`, `"hello"`, `true`, `null`.

A value is _concrete_ if it is either an atom, or a struct all of whose
field values are themselves concrete, recursively.

CUE's values also include what we normally think of as types, like `string` and
`float`.
But CUE does not distinguish between types and values; only the
relationship of values in the lattice is important.
Each CUE "type" subsumes the concrete values that one would normally think
of as part of that type.
For example, "hello" is an instance of `string`, and `42.0` is an instance of
`float`.
In addition to `string` and `float`, CUE has `null`, `int`, `bool` and `bytes`.
We informally call these CUE's "basic types".


```
false ⊑ bool
true  ⊑ bool
true  ⊑ true
5.0   ⊑ float
bool  ⊑ _
_|_   ⊑ _
_|_   ⊑ _|_

_     ⋢ _|_
_     ⋢ bool
int   ⋢ bool
bool  ⋢ int
false ⋢ true
true  ⋢ false
float ⋢ 5.0
5     ⋢ 6
```


### Unification

The _unification_ of values `a` and `b`
is defined as the greatest lower bound of `a` and `b`. (That is, the
value `u` such that `u ⊑ a` and `u ⊑ b`,
and for any other value `v` for which `v ⊑ a` and `v ⊑ b`
it holds that `v ⊑ u`.)
Since CUE values form a lattice, the unification of two CUE values is
always unique. 

These all follow from the definition of unification:
- The unification of `a` with itself is always `a`.
- The unification of values `a` and `b` where `a ⊑ b` is always `a`.
- The unification of a value with bottom is always bottom.

Unification in CUE is a [binary expression](#Operands), written `a & b`.
It is commutative and associative.
As a consequence, order of evaluation is irrelevant, a property that is key
to many of the constructs in the CUE language as well as the tooling layered
on top of it.



<!-- TODO: explicitly mention that disjunction is not a binary operation
but a definition of a single value?-->


### Disjunction

The _disjunction_ of values `a` and `b`
is defined as the least upper bound of `a` and `b`.
(That is, the value `d` such that `a ⊑ d` and `b ⊑ d`,
and for any other value `e` for which `a ⊑ e` and `b ⊑ e`,
it holds that `d ⊑ e`.)
This style of disjunctions is sometimes also referred to as sum types.
Since CUE values form a lattice, the disjunction of two CUE values is always unique.


These all follow from the definition of disjunction:
- The disjunction of `a` with itself is always `a`.
- The disjunction of a value `a` and `b` where `a ⊑ b` is always `b`.
- The disjunction of a value `a` with bottom is always `a`.
- The disjunction of two bottom values is bottom.

Disjunction in CUE is a [binary expression](#Operands), written `a | b`.
It is commutative and associative.

The unification of a disjunction with another value is equal to the disjunction
composed of the unification of this value with all of the original elements
of the disjunction.
In other words, unification distributes over disjunction.

```
(a_0 | ... |a_n) & b ==> a_0&b | ... | a_n&b.
```

```
Expression                Result
({a:1} | {b:2}) & {c:3}   {a:1, c:3} | {b:2, c:3}
(int | string) & "foo"    "foo"
("a" | "b") & "c"         _|_
```


#### Default values

One or more values in a disjunction can be _marked_
by prefixing it with a `*` ([a unary expression](#Operators)).
A bottom value cannot be marked.
When a marked value is unified, the result is also marked.
(When unification results in a single value,
the mark is dropped, as single values cannot be marked.)

A disjunction is _normalized_ if there is no unmarked element
`a` for which there is an element `b` such that `a ⊑ b`
and no marked element `c` for which there is a marked element
`d` such that `c ⊑ d`.
A disjunction literal must be normalized.

<!--
(non-normalized entries could also be implicitly marked, allowing writing
int | 1, instead of int | *1, but that can be done in a backwards
compatible way later if really desirable).

Normalization is important, as we need to account for spurious elements
For instance
"tcp" | "tcp", or
({a:1} | {b:1}) & ({a:1} | {b:2}) -> {a:1} | {a:1,b:1} | {a:1,b:2},

In the latter case, elements {a:1,b:1} and {a:1,b:2} are subsumed by {a:1}.
Note that without defaults, {a:1} | {a:1,b:1} | {a:1,b:2} is logically
identical to {a:1}.
More to the point, without normalization unifying {a:1} | {b:1} with {a:1,b:2}
results in a single value and thus resolves,
whereas unifying {a:1} | {a:1,b:1} | {a:1,b:2} with {a:1,b:2}
results in two values, and thus does not resolve.
With normalization:
({a:1} | {a:1,b:1} | {a:1,b:2}) & {a:1}          {a:1}, instead of _|_,
({a:1} | {b:1}) & {a:1}         {a:1} (instead of _|_), as {a:1,b:1} ⊑ {a:1}
-->

If a disjunction appears where a concrete value is required
(that is, as an operand or in a location where it will be emitted),
the result is, after normalization and after dropping non-marked elements
if some elements are marked,
the resulting value itself if only a single value remains or bottom otherwise.

<!--
We treat remaining marked and unmarked elements the same to have less surprises:

Unifying {a:1}|{b:1} with *{}|string produces *{a:1}|*{b:1}. It would be
surprising to have a different default for {a:1}|{b:1} and *{a:1}|*{b:1} in
this case.

Similarly, we do not unify the remaining elements to minimize the difference
between using a disjunction in cases where concrete values are required
versus otherwise.
-->

<!-- TODO: is the above definition precise enough, or perhaps too abstract?
Previously:

A default value is chosen if the disjunction is not used
in a unification or disjunction operation.
This means that, in practice, a default is chosen for almost any expression
that does not involve `&` and `|`, including slices, indices, selectors,
and all but a few explicitly marked builtin functions. -->

```
Expression                       Default
"tcp" | "udp"                    _|_ // more than one element remaining
*"tcp" | "udp"                   "tcp"
float | *1                       1
*string | 1.0                    string

(*"tcp"|"udp") & ("udp"|*"tcp")  "tcp"
(*"tcp"|"udp") & ("udp"|"tcp")   "tcp"
(*"tcp"|"udp") & "tcp"           "tcp"
(*"tcp"|"udp") & (*"udp"|"tcp")  _|_ // "tcp" & "udp"

(*true | false) & bool           true
(*true | false) & (true | false) true

{a: 1} | {b: 1}                  _|_ // more than one element remaining
{a: 1} | *{b: 1}                 {b:1}
*{a: 1} | *{b: 1}                _|_ // more than one marked element remaining
({a: 1} | {b: 1}) & {a:1}        {a:1} // after eliminating {a:1,b:1}
({a:1}|*{b:1}) & ({a:1}|*{b:1})  {b:1} // after eliminating *{a:1,b:1}
```

A disjunction always evaluates to the same default value, regardless of
the context in which the value is used.
For instance, `[1, 3][*"a" | 1]` will result in an error, as `"a"` will be
selected as the default value.

```
[1, 2][*"a" | 1]          //  _|_ // "a" is not an integer value
[1, 2][(*"a" | 1) & int]  //  2, as "a" is eliminated before choosing a default.
```


### Bottom and errors

Any evaluation error in CUE results in a bottom value, respresented by
the token '_|_'.
Bottom is an instance of every other value.
Any evaluation error is represented as bottom.

Implementations may associate error strings with different instances of bottom;
logically they all remain the same value.


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

The _null value_ is represented with the keyword `null`.
It has only one parent, top, and one child, bottom.
It is unordered with respect to any other value.

```
null_lit   = "null"
```

```
null & 8     _|_
null & _     null
null & _|_   _|_
```


### Boolean values

A _boolean type_ represents the set of Boolean truth values denoted by
the keywords `true` and `false`.
The predeclared boolean type is `bool`; it is a defined type and a separate
element in the lattice.

```
boolean_lit = "true" | "false"
```

```
bool & true          true
true & true          true
true & false         _|_
bool & (false|true)  false | true
bool & (true|false)  true | false
```


### Numeric values

The _integer type_ represents the set of all integral numbers.
The _decimal floating-point type_ represents the set of all decimal floating-point
numbers.
They are two distinct types.
The predeclared integer and decimal floating-point types are `int` and `float`;
they are defined types.

A decimal floating-point literal always has type `float`;
it is not an instance of `int` even if it is an integral number.

An integer literal has both type `int` and `float`, with the integer variant
being the default if no other constraints are applied.
Expressed in terms of disjunction and [type conversion](#conversions),
the literal `1`, for instance, is defined as `int(1) | float(1)`.
Hexadecimal, octal, and binary integer literals are always of type `int`.

Numeric literals are exact values of arbitrary precision.
If the operation permits it, numbers should be kept in arbitrary precision.

Implementation restriction: although numeric values have arbitrary precision
in the language, implementations may implement them using an internal
representation with limited precision.
That said, every implementation must:

- Represent integer values with at least 256 bits.
- Represent floating-point values, with a mantissa of at least 256 bits and
a signed binary exponent of at least 16 bits.
- Give an error if unable to represent an integer value precisely.
- Give an error if unable to represent a floating-point value due to overflow.
- Round to the nearest representable value if unable to represent
a floating-point value due to limits on precision.
These requirements apply to the result of any expression except for builtin
functions for which an unusual loss of precision must be explicitly documented.


### Strings

The _string type_ represents the set of all possible UTF-8 strings,
not allowing surrogates.
The predeclared string type is `string`; it is a defined type.

Strings are designed to be unicode-safe.
Comparison is done using canonical forms ("é" == "e\u0301").
A string element is an
[extended grapheme cluster](https://unicode.org/reports/tr29/#Grapheme_Cluster_Boundaries),
which is an approximation of a human-readable character.

The length of a string `s` (its size in bytes) can be discovered using
the built-in function len.
A string's extended grapheme cluster can be accessed by integer index
0 through len(s)-1 for any byte that is part of that grapheme cluster.

To access the individual bytes of a string one should convert it to
a sequence of bytes first.


### Bounds

A _bound_, syntactically_ a [unary expression](#Operands), defines
a (possibly infinite) disjunction of concrete values than can be represented
as a single comparison.

For any [comparison operator](#Comparison-operators) `op` except `==`,
`op a` is the disjunction of every `x` such that `x op a`.

```
2 & >=2 & <=5           // 2, where 2 is either an int or float.
2.5 & >=1 & <=5         // 2.5
2 & >=1.0 & <3.0        // 2.0
2 & 1..3.0              // 2.0
2.5 & int & >1 & <5     // _|_
2.5 & float & >1 & <5   // 2.5
int & 2 & >1.0 & <3.0   // _|_
2.5 & >=(int & 1) & <5  // _|_
>=0 & <=7 & >=3 & <=10  // >=3 & <=7
!=null & 1              // 1
>=5 & <=5               // 5
```


### Ranges

A _range type_, syntactically a [binary expression](#Operands), defines
a (possibly infinite) disjunction of concrete values that can be represented
as a contiguous range.
A concrete value `c` unifies with `a..b` if `a <= c` and `c <= b`.
Ranges can be defined on numbers and strings.

A range of numbers `a..b` defines an inclusive range for integers and
floating-point numbers.

Remember that an integer literal represents both an `int` and `float`:
```
2   & 1..5          // 2, where 2 is either an int or float.
2.5 & 1..5          // 2.5
2 & 1.0..3.0        // 2.0
2 & 1..3.0          // 2.0
2.5 & int & 1..5    // _|_
2.5 & float & 1..5  // 2.5
int & 2 & 1.0..3.0  // _|_
2.5 & (int & 1)..5  // _|_
0..7 & 3..10        // 3..7
"foo" & "a".."n"    // "foo"
```


### Structs

A _struct_ is a set of elements called _fields_, each of
which has a name, called a _label_, and value.

We say a label is defined for a struct if the struct has a field with the
corresponding label.
The value for a label `f` of struct `a` is denoted `f.a`.
A struct `a` is an instance of `b`, or `a ⊑ b`, if for any label `f`
defined for `b`, label `f` is also defined for `a` and `a.f ⊑ b.f`.
Note that if `a` is an instance of `b` it may have fields with labels that
are not defined for `b`.

The (unique) struct with no fields, written `{}`, has every struct as an
instance. It can be considered the type of all structs.

The successful unification of structs `a` and `b` is a new struct `c` which
has all fields of both `a` and `b`, where
the value of a field `f` in `c` is `a.f & b.f` if `f` is in both `a` and `b`,
or just `a.f` or `b.f` if `f` is in just `a` or `b`, respectively.
Any [references](#References) to `a` or `b`
in their respective field values need to be replaced with references to `c`.
The result of a unification is bottom (`_|_`) if any of its fields evaluates
to bottom, recursively.

A field name may also be an interpolated string.
Identifiers used in such strings are evaluated within
the scope of the struct in which the label is defined.

Syntactically, a struct literal may contain multiple fields with
the same label, the result of which is a single field with a value
that is the unification of the values of those fields.

A TemplateLabel indicates a template value that is to be unified with
the values of all fields within a struct.
The identifier of a template label binds to the field name of each
field and is visible within the template value.

```
StructLit     = "{" [ { Declaration "," } Declaration ] "}" .
Declaration   = FieldDecl | AliasDecl | ComprehensionDecl .
FieldDecl     = Label { Label } ":" Expression .

AliasDecl     = Label "=" Expression .
Label         = identifier | interpreted_string_lit | TemplateLabel .
TemplateLabel = "<" identifier ">" .
Tag           = "#" identifier [ ":" json_string ] .
```

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
{a: int} & {a: 1}            {a: int(1)}
{a: 1..7} & {a: 5..9}        {a: 5..7}
{a: 1..7, a: 5..9}           {a: 5..7}

{a: 1} & {b: 2}              {a: 1, b: 2}
{a: 1, b: int} & {b: 2}      {a: 1, b: int(2)}

{a: 1} & {a: 2}              _|_
```

In addition to fields, a struct literal may also define aliases.
Aliases name values that can be referred to within the [scope](#declarations-and-scopes) of their
definition, but are not part of the struct: aliases are irrelevant to
the partial ordering of values and are not emitted as part of any
generated data.
The name of an alias must be unique within the struct literal.

```
// The empty struct.
{}

// A struct with 3 fields and 1 alias.
{
    alias = 3

    foo: 2
    bar: "a string"

    "not an ident": 4
}
```

A field whose value is a struct with a single field may be written as
a sequence of the two field names,
followed by a colon and the value of that single field.

```
job myTask replicas: 2
```
expands to
```
job: {
    myTask: {
        replicas: 2
    }
}
```


### Lists

A list literal defines a new value of type list.
A list may be open or closed.
An open list is indicated with a `...` at the end of an element list,
optionally followed by a value for the remaining elements.

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

Lists can be thought of as structs:

```
List: *null | {
    Elem: _
    Tail: List
}
```

For closed lists, `Tail` is `null` for the last element, for open lists it is
`*null | List`, defaulting to the shortest variant.
For instance, the open list [ 1, 2, ... ] can be represented as:
```
open: List & { Elem: 1, Tail: { Elem: 2 } }
```
and the closed version of this list, [ 1, 2 ], as
```
closed: List & { Elem: 1, Tail: { Elem: 2, Tail: null } }
```

Using this representation, the subsumption rule for lists can
be derived from those of structs.
Implementations are not required to implement lists as structs.
The `Elem` and `Tail` fields are not special and `len` will not work as
expected in these cases.


## Declarations and Scopes


### Blocks

A _block_ is a possibly empty sequence of declarations.
The braces of a struct literal `{ ... }` form a block, but there are
others as well:

- The _universe block_ encompasses all CUE source text.
- Each [package](#modules-instances-and-packages) has a _package block_
  containing all CUE source text in that package.
- Each file has a _file block_ containing all CUE source text in that file.
- Each `for` and `let` clause in a [comprehension](#comprehensions)
  is considered to be its own implicit block.

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
identifier denotes the specified field, alias, or package.

CUE is lexically scoped using blocks:

1. The scope of a [predeclared identifier](#predeclared-identifiers) is the universe block.
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
the package name does not appear in any scope.
Its purpose is to identify the files belonging to the same package
and to specify the default name for import declarations.


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
bytes     Any vallid byte sequence

Derived   Value
number    int | float
uint      0..int
uint8     0..255
int8      -128..127
uint16    0..65536
int16     -32_768...32_767
rune      0..0x10FFFF
uint32    0..4_294_967_296
int32     -2_147_483_648..2_147_483_647
uint64    0..18_446_744_073_709_551_615
int64     -9_223_372_036_854_775_808..9_223_372_036_854_775_807
uint128   340_282_366_920_938_463_463_374_607_431_768_211_455
int128    -170_141_183_460_469_231_731_687_303_715_884_105_728..
           170_141_183_460_469_231_731_687_303_715_884_105_727
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
Quoted labels that start with an underscore are emitted, however.

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

A field declaration binds a label (the name of the field) to an expression.
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


## Expressions

An expression specifies the computation of a value by applying operators and
built-in functions to operands.


### Operands

Operands denote the elementary values in an expression.
An operand may be a literal, a (possibly qualified) identifier denoting
field, alias, or a parenthesized expression.

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
A default expression is only valid as an operand to a disjunction.

<!-- TODO(mpvl)
	Conversion |
-->
```
PrimaryExpr =
	Operand |
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

denotes the element of the list, string, bytes, or struct `a` indexed by `x`.
The value `x` is called the index or field name, respectively.
The following rules apply:

If `a` is not a struct:

- the index `x` must be a concrete integer.
  If `x` is a disjunction, the default, if any will be selected without unifying
  `x` with `int` beforehand.
- the index `x` is in range if `0 <= x < len(a)`, otherwise it is out of range

The result of `a[x]` is

for `a` of list type (including single quoted strings, which are lists of bytes):

- the list element at index `x`, if `x` is within range, where only the
  explicitly defined values of an open-ended list are considered
- bottom (an error), otherwise

for `a` of string type:

- the grapheme cluster at the `x`th byte (type string), if `x` is within range
  where `x` may match any byte of the grapheme cluster
- bottom (an error), otherwise

for `a` of struct type:

- the value of the field named `x` of struct `a`, if this field exists
- bottom (an error), otherwise

```
[ 1, 2 ][1]     // 2
[ 1, 2 ][2]     // _|_
[ 1, 2, ...][2] // _|_
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
constructs a substring or slice. The indices `low` and `high` must be
concrete integers and select
which elements of operand `a` appear in the result.
The result has indices starting at 0 and length equal to `high` - `low`.
After slicing the list `a`
<!-- TODO(jba): how does slicing open lists work? -->

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

binary_op  = "|" | "&" | "||" | "&&" | "==" | rel_op | add_op | mul_op | ".."  .
rel_op     = "!=" | "<" | "<=" | ">" | ">=" .
add_op     = "+" | "-" .
mul_op     = "*" | "/" | "%" | "div" | "mod" | "quo" | "rem" .
unary_op   = "+" | "-" | "!" | "*" | rel_op .
```
<!-- TODO: consider adding unary_op: "<" | "<=" | ">" | ">=" -->

Comparisons are discussed [elsewhere](#Comparison-operators).
For any binary operators, the operand types must unify.
<!-- TODO: durations
 unless the operation involves durations.

Except for duration operations, if one operand is an untyped [literal] and the
other operand is not, the constant is [converted] to the type of the other
operand.
-->


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
    7             *  /  %  div mod quo rem
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
`+` and `*` also apply to lists and strings.
`/` and `%` only apply to decimal floating-point types and
`div`, `mod`, `quo`, and `rem` only apply to integer types.

```
+    sum                    integers, floats, lists, strings, bytes
-    difference             integers, floats
*    product                integers, floats, lists, strings, bytes
/    quotient               floats
%    remainder              floats
div  division               integers
mod  modulo                 integers
quo  quotient               integers
rem  remainder              integers
```


#### Integer operators

For two integer values `x` and `y`,
the integer quotient `q = x div y` and remainder `r = x mod y `
implement Euclidean division and
satisfy the following relationship:

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
implement truncated division and
satisfy the following relationship:

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
<!-- TODO: consider making it +/- Inf -->

An implementation may combine multiple floating-point operations into a single
fused operation, possibly across statements, and produce a result that differs
from the value obtained by executing and rounding the instructions individually.


#### List operators

Lists can be concatenated using the `+` operator.
For lists `a` and `b`,
```
a + b
```
will produce an open list if `b` is open.
If list `a` is open, its default value, the shortest variant, is selected.

```
[ 1, 2 ]      + [ 3, 4 ]       // [ 1, 2, 3, 4 ]
[ 1, 2, ... ] + [ 3, 4 ]       // [ 1, 2, 3, 4 ]
[ 1, 2 ]      + [ 3, 4, ... ]  // [ 1, 2, 3, 4, ... ]
```

Lists can be multiplied with a positive `int` using the `*` operator
to create a repeated the list by the indicated number.
```
3*[1,2]         // [1, 2, 1, 2, 1, 2]
3*[1, 2, ...]   // [1, 2, 1, 2, 1 ,2]
[byte]*4        // [byte, byte, byte, byte]
```

<!-- TODO(mpvl): should we allow multiplication with a range?
If so, how does one specify a list with a range of possible lengths?

Suggestion from jba:
Multiplication should distribute over disjunction,
so int(1)..int(3) * [x] = [x] | [x, x] | [x, x, x].
The hard part is figuring out what 1..3 * [x] means,
since 1..3 includes many floats.
(mpvl: could constrain arguments to parameter types, but needs to be
done consistently.)
-->


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
<!-- jba: Do these work for byte sequences? If not, why not? -->

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

- Null is comparable with itself and any other type.
  Two null values are always equal, null is unequal with anything else.
- Boolean values are comparable.
  Two boolean values are equal if they are either both true or both false.
- Integer values are comparable and ordered, in the usual way.
- Floating-point values are comparable and ordered, as per the definitions
  for binary coded decimals in the IEEE-754-2008 standard.
- String values are comparable and ordered, lexically byte-wise after
  normalization to Unicode normal form NFC.
- Struct are not comparable.
- Lists are not comparable.
```
a: 3 < 4       // true
b: null == 2   // false
c: null != {}  // true
d: {} == {}    // _|_: structs are not comparable against structs
```

<!-- jba
I think I know what `3 < a` should mean if

    a: 1..5
    
It should be a constraint on `a` that can be evaluated once `a`'s value is known more precisely.

But what does `3 < 1..5` mean? We'll never get more information, so it must have a definite value.
-->

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

<!--- TODO(mpvl): conversions
### Conversions
Conversions are expressions of the form `T(x)` where `T` and `x` are
expressions.
The result is always an instance of `T`.

```
Conversion = Expression "(" Expression [ "," ] ")" .
```
--->
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
<!---

A conversion is always allowed if `x` is an instance of `T`.

If `T` and `x` of different underlying type, a conversion is allowed if
`x` can be converted to a value `x'` of `T`'s type, and
`x'` is an instance of `T`.
A value `x` can be converted to the type of `T` in any of these cases:

- `x` is a struct and is subsumed by `T`.
- `x` and `T` are both integer or floating points.
- `x` is an integer or a byte sequence and `T` is a string.
- `x` is a string and `T` is a byte sequence.

Specific rules apply to conversions between numeric types, structs,
or to and from a string type. These conversions may change the representation
of `x`.
All other conversions only change the type but not the representation of x.


#### Conversions between numeric ranges
For the conversion of numeric values, the following rules apply:

1. Any integer value can be converted into any other integer value
   provided that it is within range.
2. When converting a decimal floating-point number to an integer, the fraction
   is discarded (truncation towards zero). TODO: or disallow truncating?

```
a: uint16(int(1000))  // uint16(1000)
b: uint8(1000)        // _|_ // overflow
c: int(2.5)           // 2  TODO: TBD
```


#### Conversions to and from a string type

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

#### Conversions between list types

Conversions between list types are possible only if `T` strictly subsumes `x`
and the result will be the unification of `T` and `x`.

If we introduce named types this would be different from IP & [10, ...]

Consider removing this until it has a different meaning.

```
IP:        4*[byte]
Private10: IP([10, ...])  // [10, byte, byte, byte]
```

#### Conversions between struct types

A conversion from `x` to `T`
is applied using the following rules:

1. `x` must be an instance of `T`,
2. all fields defined for `x` that are not defined for `T` are removed from
  the result of the conversion, recursively.

<!-- jba: I don't think you say anywhere that the matching fields are unified.
mpvl: they are not, x must be an instance of T, in which case x == T&x,
so unification would be unnecessary.
-->
<!--
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
-->

### Calls

Calls can be made to core library functions, called builtins.
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

In a function call, the function value and arguments are evaluated in the usual
order.
After they are evaluated, the parameters of the call are passed by value
to the function and the called function begins execution.
The return parameters
of the function are passed by value back to the calling function when the
function returns.


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
If there are two identifiers, the first value will be the key or index,
if available, and the second will be the value.

An `if` clause, or guard, specifies an expression that terminates the current
iteration if it evaluates to false.

The `let` clause binds the result of an expression to the defined identifier
in a new scope.

A current iteration is said to complete if the innermost block of the clause
sequence is reached.

_List comprehensions_ specify a single expression that is evaluated and included
in the list for each completed iteration.

_Field comprehensions_ follow a `Field` with a clause sequence, where the
label and value of the field are evaluated for each iteration.
The label must be an identifier or interpreted_string_lit, where the
later may be a string interpolation that refers to the identifiers defined
in the clauses.
Values of iterations that map to the same label unify into a single field.

```
ComprehensionDecl   = Field [ "<-" ] Clauses .
ListComprehension   = "[" Expression [ "<-" ] Clauses "]" .

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

String interpolation allows constructing strings by replacing placeholder
expressions with their string representation.
String interpolation may be used in single- and double-quoted strings, as well
as their multiline equivalent.

A placeholder consists of "\(" followed by an expression and a ")". The
expression is evaluated within the scope within which the string is defined.

```
a: "World"
b: "Hello \( a )!" // Hello World!
```


## Builtin Functions

Built-in functions are predeclared. They are called like any other function.


### `len`

The built-in function `len` takes arguments of various types and return
a result of type int.

```
Argument type    Result

string            string length in bytes
bytes             length of byte sequence
list              list length, smallest length for an open list
struct            number of distinct fields
```

```
Expression           Result
len("Hellø")         6
len([1, 2, 3])       3
len([1, 2, ...])     2
len({a:1, b:2})      2
```


## Cycles

Implementations are required to interpret or reject cycles encountered
during evaluation according to the rules in this section.


### Reference cycles

A _reference cycle_ occurs if a field references itself, either directly or
indirectly.

```
// x references itself
x: x

// indirect cycles
b: c
c: d
d: b
```

Implementations should report these as an error except in the following cases:


#### Expressions that unify an atom with an expression

An expression of the form `a & e`, where `a` is an atom
and `e` is an expression, always evaluates to `a` or bottom.
As it does not matter how we fail, we can assume the result to be `a`
and validate after the field in which the expression occurs has been evaluated
that `a == e`.

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


#### Field values

A field value of the form `r & v`,
where `r` evaluates to a reference cycle and `v` is a value,
evaluates to `v`.
Unification is idempotent and unifying a value with itself ad infinitum,
which is what the cycle represents, results in this value.
Implementations should detect cycles of this kind, ignore `r`,
and take `v` as the result of unification.
<!-- Tomabechi's graph unification algorithm
can detect such cycles at near-zero cost. -->

```
Configuration    Evaluated
//    c           Cycles in nodes of type struct evaluate
//  ↙︎   ↖         to the fixed point of unifying their
// a  →  b        values ad infinitum.

a: b & { x: 1 }   // a: { x: 1, y: 2, z: 3 }
b: c & { y: 2 }   // b: { x: 1, y: 2, z: 3 }
c: a & { z: 3 }   // c: { x: 1, y: 2, z: 3 }

// resolve a             b & {x:1}
// substitute b          c & {y:2} & {x:1}
// substitute c          a & {z:3} & {y:2} & {x:1}
// eliminate a (cycle)   {z:3} & {y:2} & {x:1}
// simplify              {x:1,y:2,z:3}
```

This rule also applies to field values that are disjunctions of unification
operations of the above form.

```
a: b&{x:1} | {y:1}  // {x:1,y:3,z:2} | {y:1}
b: {x:2} | c&{z:2}  // {x:2} | {x:1,y:3,z:2}
c: a&{y:3} | {z:3}  // {x:1,y:3,z:2} | {z:3}


// resolving a           b&{x:1} | {y:1}
// substitute b          ({x:2} | c&{z:2})&{x:1} | {y:1}
// simplify              c&{z:2}&{x:1} | {y:1}
// substitute c          (a&{y:3} | {z:3})&{z:2}&{x:1} | {y:1}
// simplify              a&{y:3}&{z:2}&{x:1} | {y:1}
// eliminate a (cycle)   {y:3}&{z:2}&{x:1} | {y:1}
// expand                {x:1,y:3,z:2} | {y:1}
```

Note that all nodes that form a reference cycle to form a struct will evaluate
to the same value.
If a field value is a disjunction, any element that is part of a cycle will
evaluate to this value.


### Structural cycles

CUE disallows infinite structures.
Implementations must report an error when encountering such declarations.

<!-- for instance using an occurs check -->

```
// Disallowed: a list of infinite length with all elements being 1.
list: {
    head: 1
    tail: list
}

// Disallowed: another infinite structure (a:{b:{d:{b:{d:{...}}}}}, ...).
a: {
    b: c
}
c: {
    d: a
}
```

It is allowed for a value to define an infinite set of possibilities
without evaluating to an infinite structure itself.

```
// List defines a list of arbitrary length (default null).
List: *null | {
    head: _
    tail: List
}
```

<!--
Consider banning any construct that makes CUE not having a linear
running time expressed in the number of nodes in the output.

This would require restricting constructs like:

(fib&{n:2}).out

fib: {
        n: int

        out: (fib&{n:n-2}).out + (fib&{n:n-1}).out if n >= 2
        out: fib({n:n-2}).out + fib({n:n-1}).out if n >= 2
        out: n if n < 2
}

-->
<!--
### Unused fields

TODO: rules for detection of unused fields

1. Any alias value must be used
-->


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
A _module_ defines a tree of directories, rooted at the _module root_.

All source files within a module with the same package belong to the same
package.
<!-- jba: I can't make sense of the above sentence. -->
A module may define multiple packages.

An _instance_ of a package is any subset of files belonging
to the same package.
<!-- jba: Are you saying that -->
<!-- if I have a package with files a, b and c, then there are 8 instances of -->
<!-- that package, some of which are {a, b}, {c}, {b, c}, and so on? What's the -->
<!-- purpose of that definition? -->
It is interpreted as the concatenation of these files.

An implementation may impose conventions on the layout of package files
to determine which files of a package belongs to an instance.
For example, an instance may be defined as the subset of package files
belonging to a directory and all its ancestors.
<!-- jba: OK, that helps a little, but I still don't see what the purpose is. -->

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
<!-- jba: Can you omit this feature? It's likely to only decrease readability,
as we know from Go. -->

The interpretation of the ImportPath is implementation-dependent but it is
typically either the path of a builtin package or a fully qualifying location
of an instance within a source code repository.

Implementation restriction: An interpreter may restrict ImportPaths to non-empty
strings using only characters belonging to Unicode's L, M, N, P, and S general
categories (the Graphic characters without spaces) and may also exclude the
characters !"#$%&'()*,:;<=>?[\]^`{|} and the Unicode replacement character
U+FFFD.

Assume we have package containing the package clause "package math",
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





