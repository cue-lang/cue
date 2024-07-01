// Copyright 2024 CUE Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     https://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package apicheck checks CUE values for backwards compatibility according to
// CUE's backwards compatibility guidelines. THIS PACKAGE IS EXPERIMENTAL.
//
// Backwards compatibility guidelines aim to minimize the impact of changes to
// an API on existing consumers of that API.
//
// It is generally impossible to guarantee that changes to an API will not break
// a consumer. For instance, a common extension to an API is to add a new field
// to an object. CUE typically reports an error if a users uses a field that was
// previously undefined within an object, so adding a new field is generally
// considered to be safe. However, if a user embeds and extends the object and
// adds new fields, future changes to the API may break this user.
//
// The aim of backwards compatibility is not to prevent such breakages at all
// cost, but rather to define a contract that makes a clear division of the
// responsibilities to avoid breakages between API producers and consumers. For
// instance, in the example above, the producer of the API may not prohibit
// values of a field in a newer version that were previously allowed, while the
// consumer is responsible for possible breakages that result using a
// third-party API as an embedding.
//
// # Terminology and Principles
//
// ## Principles
//
// The compatibility rules are based on a few principles:
//
//	P1) A CUE value that unifies with version v1.1 of an API should also unify
//	    with a version v1.2 of that same API.
//	P2) If concrete data passes validation against a version v1.1 of an API, it
//	    should also pass validation against a version v1.2 of that same API.
//	P3) If concrete data successfully unifies with a version v1.1 of
//	    an API, it should also unify with a version v1.2 of that same API.
//	P4) A stricter version of P3 is that it should unify to give the same
//	    outcome for a newer version.
//	P5) An API represents a contract of the behavior of a real world entity.
//	    The source of truth is the real world entity, not the API. This means
//	    that breaking changes should be allowed if they correct previously
//	    erroneous specifications.
//	P6) Backwards compatibility guarantees are not given if a consumer uses any
//	    operation to assign values to regular fields that are derived from:
//	    attributes, comments, pattern constraints, or optional and required
//	    fields.
//
// ## Bug fix examples
//
// In some cases a breaking change should be allowed. An API defines a contract
// of how something in the real world behaves. If an API incorrectly describes
// this real-world behavior we consider this a bug.
//
// Let's say a field `foo` is defined to be of type `string“. In practice,
// though, the server that implements the respective API only accepts values of
// type `int` for this field. In this case it makes sense to allow a change to
// the respective CUE code to fix this. This can break existing code, but such
// code would break in real life as well: this breaking change is justified and
// even desirable.
//
// Another breaking change that may be allowed is to restrict the possible
// values of a field. Let's say the a field is defined as `foo: int`. We know,
// however, that in practice, the corresponding server only accepts values from
// 0 to 10. It may be desirable to tighten up the API to reflect this. Insofar
// this breaks existing code, it is breaking code that would already not
// function in practice.
//
// Note that, in the latter case, future APIs may allow a larger value range for
// such an API, so such restrictions may need to be widened for future releases.
// Because such restrictions may be error prone if they are not maintained by
// the producer of the API, an API producer may chose to deliberately let an API
// allow wider values than are permitted in reality.
//
// ## Attributes, Comments, and Constraints
//
// Currently, there is no direct way in CUE to programmatically access comments,
// attributes, or even the value associated with field constraints. Nonetheless,
// there are indirect ways to access them. For instance, if a field is optional,
// it is possible to construct CUE that accesses the underlying value. Comments
// could be made accessible through a OpenAPI encoding package, for instance.
// Attributes could influence values in similar ways.
//
// In all these cases we consider these values to be outside the realm of the
// backwards compatibility contract. This means that the responsibility of usage
// of such means of introspection is on the consumer of the API.
//
// This only applies to minor level changes. For patch-level compatibility,
// changing values as a result of such introspections break compatibility.
//
// # Compatibility of CUE values
//
// Unless noted otherwise, compatibility is defined here to what corresponds to
// the minor level of semantic versioning.
//
// ## Schemas
//
// A principle characteristic of CUE is that it partially orders all its values.
// This principle is used to define compatibility between schemas. Basically, a
// Schema of newer API is compatible an older version of itself if the older one
// is an instance of the newer one. Basically, a Schema is allowed to "widen",
// but not "narrow".
//
// There are some further exceptions and details to the usual ordering that we
// will discuss here.
//
// ### Introducing new optional fields
//
// One important caveat is that, for the purpose of comparing APIs, we define
// non-existent fields in a definition as optional fields with an error value
// ("bottom"). As a consequence, since any value allows error as an instance, we
// effectively allow APIs to introduce new fields, even though this is not
// allowed during normal execution of a CUE program.
//
// For those familiar with protocol buffers, this duality of the two different
// interpretations is analogous to the rule that unknown fields in incoming
// messages are ignored, while if an unknown field is set in a protobuf message
// within, say, a Go program, it results in a compilation error.
//
// Note that introducing new optional fields in definitions is generally
// compatible with all of the principles, as the usage of such fields previously
// would always result in errors and the addition of such fields cannot cannot
// result in new values, keeping Principle P6 in mind.
//
// ### Ordering of optional, required, and regular fields
//
// CUE normally orders regular, required, and optional fields from more to less
// specific. However, for practical concerns surrounding compatibility, any
// change in the status of fields is considered a breaking change.
//
// This means we disallow all of the following modifications that would normally
// be allowed:
//
//	   v1.1        -> v1.2
//	1. {foo: int}  -> {foo!: int}
//	2. {foo: int}  -> {foo?: int}
//	3. {foo!: int} -> {foo?: int}
//
// Making a previously regular field required (1) is clearly a breaking change
// when a consumer does not provide a concrete value for `foo`, violating
// Principle P2 and P3. Also making a previously provided field optional (2)
// seems tenuous, as it may change the semantics of an exported value, violating
// Principle P4. The remaining option, which on the surface seems safe, is to
// change a required field to an optional field (3). Such an API change is
// generally discouraged (see guidelines in Protobuf land, for instance). We
// disallow this as well for concistency and simplicity.
//
// ### Defaults and concrete fields
//
// The guidelines (see below) recommend to not use defaults in schemas. If
// defaults are nonetheless crucial to the semantics of the API, apicheck
// defines some rules to clarify the responsibilities regarding backwards
// compatibility.
//
// More generally, a compatibility problem may arise if an older version of an
// API defines regular fields with concrete values, regardless of whether they
// originate from a default. If such a value is relaxed to the point it is no
// longer concrete, this will result in a breaking change, as it may turn
// previously concrete values into non-concrete values, violating Principle P2
// and P3.
//
// For this reason, we require concrete values of regular fields to be equal
// between versions, with the exception that a new version may assign concrete
// values to previously invalid fields within that default value. A less strict
// version of this rule is to allow introducting concrete values, allowing it to
// violate Principle P4.
//
// ## Templates
//
// Templates can serve several purposes. They can be used to provide default
// values for schemas, they may taylor a schema for a specific use case, or they
// may convey certain policies to be applied to an API, among other things. We
// apply the same compatibility rules for all these cases.
//
// A template is compatible with a newer version of the same template if their
// underlying schema are compatible. The underlying schema of a template is
// defined as any schema that is unified with the template.
//
// So for the following template `Foo`,
//
//	Foo: #Foo & {
//	    a: *1 | int
//	}
//
// only the underlying schema #Foo is considered for compatibility, not the
// values it is unified with.
//
// Clearly, changing template values can break users of this template. Changes
// should therefore be made with care. In some cases, such as with policy
// changes, breakages can even be desirable.
//
// ## Data
//
// Data values are subject to the same rules as templates, but are also required
// to be concrete.
//
// # Package-level Compatibility
//
// This section defines rules for assigning compatibility types, as defined in
// the previous section, to the values of a package.
//
// A package is either interpreted as a schema, a template, data or a collection
// of CUE values, where the latter is fallback or default value. Each is
// explained below. It is considered to be a breaking change for a value to be
// assigned a different interpretation.
//
// ## Schema Package
//
// If the top-level of a package only has constraint fields (optional fields,
// required fields or pattern constraints), or it has top-level value attribute
// `@api(schema)`, it is interpreted as a schema in its entirety
//
// ## Template Packages
//
// If a package has a top-level value attribute `@api(template)`, it is
// interpreted as a template.
//
// ### Schemas defined in templates
//
// Definitions that occur within templates are ignored.
//
// ## Data Packages
//
// If a package has a top-level value attribute `@api(data)`, it is interpreted
// as data.
//
// ## Collection of CUE values
//
// The default interpretation of a package is as a collection of CUE values.
// Here all top-level definitions are considered to be schema. Fields that start
// with a capital letter are considered to be templates. All other regular
// fields are considered to be data.
//
// TBD: we could potentially consider any field that unifies with a schema (at
// the top level, not as an embedding) to be a template of this schema, even if
// is a definitions.
//
// Any top-level constraints and hidden fields are ignored. (TODO: is this
// correct?)
//
// The interpretation of any of the top-level fields can be overridden by the
// use of the `@api` attribute, analogous to the rules for packages.
//
// # Module-level Compatibility
//
// Backwards compatibility between modules is defined in terms of the set of
// packages they contains as well as the pairwise compatibility of these
// packages.
//
// A newer module is compatible at the patch level with an older module if the
// set of packages did not change and all packages are compatible at the patch
// level.
//
// A module is compatible at the minor level if all packages defined by the
// older module exist in the newer module and are at least compatible at the
// minor level.
//
// # Guidelines and Recommendations
//
// ## Schema
//
// Schemas should generally only consist of constraints. In other words, they
// should only contain pattern constraints, optional fields, and required
// fields. Regular fields should generally be avoided in schemas.
//
// Using regular fields with concrete values is akin to using defaults. Using
// this instead of, for instance, a required field allows an API to be used to
// make a concrete value value, but it prevents the API from being used to
// validate that an existing concrete value is a valid value.
//
// Using regular fields with non-concrete values make regular fields behave like
// required fields, if a schema is used to create concrete data. Using regular
// fields this way, however, obfuscates the intention that a field is required.
//
// To provide concrete values or default values, consider the Default Pattern.
//
// ## The Defaults Pattern
//
// It is generally not recommended to include default values, or predefined
// concrete values, in schemas. If one must provide such values, it is
// recommended to pair the schema with a namesake template that provides these
// values instead.
//
// Consider this CUE
//
//	#Foo: {
//	    kind!: "graph"
//
//	    nodes?:   [...int]
//
//	    // initial marks the initial set of nodes. This must be a subset of
//	    // nodes. It defaults to the first node of nodes.
//	    initial?: int
//	}
//
//	Foo: #Foo & {
//	    // Since kind must always be graph, we can fix the value here.
//	    kind: "graph"
//
//	    // The default initial set is the first node in nodes.
//	    nodes?:  _
//	    initial: *nodes[:1] | _
//	}
//
// Here we have a schema `#Foo` with only optional and required fields and
// template `Foo` that provides default values for the optional fields that are
// reflected in the comments of the schema.
//
// As a convention, we use `Foo` for templates derived from `#Foo` for templates
// that only fill out defaults that describe behavior of the server, such as
// those reflected in the comments of `#Foo`. If a template adds any other
// conveniences, we suggest to use a derivative name, such as `Foo_MyDefaults`.
//
// Note also that, even though the value for `kind` is always `"graph"`, we did
// not make `kind` a regular field in schema `#Foo`. This would certainly work,
// but the resulting schema can only be used to complete a value to be valid,
// it can no longer be used to check if a concrete value is valid.
//
// Similarly, the original schema could have provided the default for `initial`.
// But one has to ask for what purpose? Knowing the default is handy to know how
// something is used. But if one just wants to validate a message that is sent
// over the wire, adding the defaults will result in unnecessary traffic.
//
// Note that this distinction has an analogy in JSON Schema and OpenAPI. There,
// default values deliberately do not take part in validation. The standard
// makes recommendations along similar lines for exactly this purpose.
//
// In practice, it can even be helpful to have a template which only adds
// default values versus one that fills out required discriminator fields. For
// instance:
//
//	// Foo defines all boilerplate fields of #Foo that the user needs to
//	// specify.
//	Foo: #Foo & {
//	    kind: "graph"
//	}
//
//	// Foo_Defaults defines all boilerplate fields of #Foo and additionally all
//	// default values that define how a field is interpreted if it is left out.
//	Foo_Defaults: #Foo & {
//	    nodes?:  _
//	    initial: *nodes[:1] | _
//	}
//
// This separation only applies if there are fields that are not required, so it
// may not be commonly necessary.
//
// ## Embedding
//
// Embedding is a powerful feature of CUE that allows one to extend a schema by
// adding fields to it. From an API perspective, however, a schema makes itself
// prone to breaking if it uses embedding: if the schema adds a field that is
// later also added by the embedded schema, the API may break.
//
// To avoid this, it is recommended to only use embedding in schemas written by
// the same author, or at least where the author has control over the definition
// of the embedded schema. Ideally, these embedded schema are defined within the
// same module.
package apicheck
