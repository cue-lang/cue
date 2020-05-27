# Implementing CUE


> NOTE: this is a working document attempting to describe CUE in a way
> relatable to existing graph unification systems. It is mostly
> redundant to [the spec](./spec.md). Unless one is interested in
> understanding how to implement CUE or how it relates to the existing
> body of research, read the spec instead.


CUE is modeled after typed feature structure and graph unification systems
such as LKB.
There is a wealth of research related to such systems and graph unification in
general.
This document describes the core semantics of CUE in a notation
that allows relating it to this existing body of research.


## Background

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

## Formal Definition


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

### Definition of basic values

> A _basic value_ is any CUE value that is not a struct (or, by
> extension, a list).
> All basic values are partially ordered in a lattice, such that for any
> basic value `a` and `b` there is a unique greatest lower bound
> defined for the subsumption relation `a ⊑ b`.

```
Basic values
null
true
bool
3.14
string
"Hello"
>=0
<8
re("Hello .*!")
```

The basic values correspond to their respective types defined earlier.

Struct (and by extension lists), are represented by the abstract notion of
a typed feature structure.
Each node in a configuration, including the root node,
is associated with a constraint.


### Definition of a typed feature structures and substructures

<!-- jba: This isn't adding understanding. I'd rather you omitted it and
   added a bit of rigor to the above spec. Or at a minimum, translate the
   formalism into the terms you use above.
-->

> A typed feature structure_ defined for a finite set of labels `Label`
> is directed acyclic graph with labeled
> arcs and values, represented by a tuple `C = <Q, q0, υ, δ>`, where
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
> 1. every basic value: `P ⊆ T`
> 1. every constraint in `𝒞`<sub>`Label`</sub> is a term: `𝒞`<sub>`Label`</sub>` ⊆ T`
>    a _reference_ may refer to any substructure of `C`.
> 1. for every `n` values `t₁, ..., tₙ`, and every `n`-ary function symbol
>    `f ∈ F_n`, the value `f(t₁,...,tₙ) ∈ T`.
>


This definition has been taken and modified from [Carpenter, 1992]
and [Copestake, 2002].

Without loss of generality, we will henceforth assume that the given set
of labels is constant and denote `𝒞`<sub>`Label`</sub> as `𝒞`.

In CUE configurations, the abstract constraints implicated by `υ`
are CUE expressions.
Literal structs can be treated as part of the original typed feature structure
and do not need evaluation.
Any other expression is evaluated and unified with existing values of that node.

References in expressions refer to other nodes within the `C` and represent
a copy of the substructure `C'` of `C` rooted at these nodes.
Any references occurring in terms assigned to nodes of `C'` are be updated to
point to the equivalent node in a copy of `C'`.
<!-- TODO: define formally. Right now this is implied already by the
definition of evaluation functions and unification: unifying
the original TFS' structure of the constraint with the current node
preserves the structure of the original graph by definition.
This is getting very implicit, though.
-->
The functions defined by `F` correspond to the binary and unary operators
and interpolation construct of CUE, as well as builtin functions.

CUE allows duplicate labels within a struct, while the definition of
typed feature structures does not.
A duplicate label `l` with respective values `a` and `b` is represented in
a constraint as a single label with term `&(a, b)`,
the unification of `a` and `b`.
Multiple labels may be recursively combined in any order.

<!-- unnecessary, probably.
#### Definition of evaluated value

> A fully evaluated value, `T_evaluated ⊆ T` is a subset of `T` consisting
> only of atoms, typed attribute structures and constraint functions.
>
> A value is called _ground_ if it is an atom or typed attribute structure.

#### Unification of evaluated values

> A fully evaluated value, `T_evaluated ⊆ T` is a subset of `T` consisting
> only of atoms, typed attribute structures and constraint functions.
>
> A value is called _ground_ if it is an atom or typed attribute structure.
-->

### Definition of subsumption and unification on typed attribute structure

> For a given collection of constraints `𝒞`,
> we define `π ≡`<sub>`C`</sub> `π'` to mean that typed feature structure `C ∈ 𝒞`
> contains path equivalence between the paths `π` and `π'`
> (i.e. `δ(q0, π) = δ(q0, π')`, where `q0` is the root node of `C`);
> and `𝒫`<sub>`C`</sub>`(π) = c` to mean that
> the typed feature structure at the path `π` in `C`
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

<!-- jba: So what does this get you that you don't already have from the
various "instance-of" definitions in the main spec? I thought those were
sufficiently precise. Although I admit that references and cycles
are still unclear to me. -->

Like with the subsumption relation for basic values,
the subsumption relation for constraints determines the mutual placement
of constraints within the partial order of all values.


### Evaluation function

> The evaluation function is given by `E: T -> 𝒞`.
> The unification of two typed feature structures is evaluated as defined above.
> All other functions are evaluated according to the definitions found earlier
> in this spec.
> An error is indicated by `_|_`.

#### Definition of well-formedness

> We say that a given typed feature structure `C = <Q, q0, υ, δ> ∈ 𝒞` is
> a _well-formed_ typed feature structure if and only if for all nodes `q ∈ Q`,
> the substructure `C'` rooted at `q`,
> is such that `E(υ(q)) ∈ 𝒞` and `C' = <Q', q, δ', υ'> ⊑ E(υ(q))`.

<!-- Also, like Copestake, define appropriate features?
Appropriate features are useful for detecting unused variables.

Appropriate features could be introduced by distinguishing between:

a: MyStruct // appropriate features are MyStruct
a: {a : 1}

and

a: MyStruct & { a: 1 } // appropriate features are those of MyStruct + 'a'

This is way too subtle, though.

Alternatively: use Haskell's approach:

#a: MyStruct // define a to be MyStruct any other features are allowed but
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

### References
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
- [1992] Hideto Tomabechi, "Quasi-destructive graph unifications with structure-
   sharing."; In Proceedings of the 15th International Conference on
   Computational Linguistics (COLING-92), Nantes, France.
- [2001] Marcel van Lohuizen, "Memory-efficient and thread-safe
  quasi-destructive graph unification."; In Proceedings of the 38th Meeting of
  the Association for Computational Linguistics. Hong Kong, China.


## Implementation

The _evaluation_ of a CUE configuration `C` is defined as the process of
making `C` well-formed.


This section does not define any operational semantics.
As the unification operation is communitive, transitive, and reflexive,
implementations have a considerable amount of leeway in
choosing an evaluation strategy.
Although most algorithms for the unification of typed attribute structure
that have been proposed are near `O(n)`, there can be considerable performance
benefits of choosing one of the many proposed evaluation strategies over the
other.
Implementations will need to be verified against the above formal definition.


### Constraint functions

A _constraint function_ is a unary function `f` which for any input `a` only
returns values that are an instance of `a`. For instance, the constraint
function `f` for `string` returns `"foo"` for `f("foo")` and `_|_` for `f(1)`.
Constraint functions may take other constraint functions as arguments to
produce a more restricting constraint function.
For instance, the constraint function `f` for `<=8` returns `5` for `f(5)`,
`>=5 & <=8` for `f(>=5)`, and `_|_` for `f("foo")`.


Constraint functions play a special role in unification.
The unification function `&(a, b)` is defined as

- `a & b` if `a` and `b` are two atoms
- `a & b` if `a` and `b` are two nodes, respresenting struct
- `a(b)` or `b(a)` if either `a` or `b` is a constraint function, respectively.

Implementations are free to pick which constraint function is applied if
both `a` and `b` are constraint functions, as the properties of unification
will ensure this produces identical results.


### References

A distinguising feature of CUE's unification algorithm is the use of references.
In conventional graph unification for typed feature structures, the structures
that are unified into the existing graph are independent and pre-evaluated.
In CUE, the typed feature structures indicated by references may still need to
be evaluated.
Some conventional evaluation strategies may not cope well with references that
refer to each other.
The simple solution is to deploy a breadth-first evaluation strategy, rather than
the more traditional depth-first approach.
Other approaches are possible, however, and implementations are free to choose
which approach is deployed.

