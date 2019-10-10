// Protocol Buffers - Google's data interchange format
// Copyright 2008 Google Inc.  All rights reserved.
// https://developers.google.com/protocol-buffers/
//
// Redistribution and use in source and binary forms, with or without
// modification, are permitted provided that the following conditions are
// met:
//
//     * Redistributions of source code must retain the above copyright
// notice, this list of conditions and the following disclaimer.
//     * Redistributions in binary form must reproduce the above
// copyright notice, this list of conditions and the following disclaimer
// in the documentation and/or other materials provided with the
// distribution.
//     * Neither the name of Google Inc. nor the names of its
// contributors may be used to endorse or promote products derived from
// this software without specific prior written permission.
//
// THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS
// "AS IS" AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT
// LIMITED TO, THE IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR
// A PARTICULAR PURPOSE ARE DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT
// OWNER OR CONTRIBUTORS BE LIABLE FOR ANY DIRECT, INDIRECT, INCIDENTAL,
// SPECIAL, EXEMPLARY, OR CONSEQUENTIAL DAMAGES (INCLUDING, BUT NOT
// LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR SERVICES; LOSS OF USE,
// DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER CAUSED AND ON ANY
// THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY, OR TORT
// (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE
// OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.

// Author: kenton@google.com (Kenton Varda)
//  Based on original Protocol Buffers design by
//  Sanjay Ghemawat, Jeff Dean, and others.
//
// The messages in this file describe the definitions found in .proto files.
// A valid .proto file can be translated directly to a FileDescriptorProto
// without any other information (e.g. without reading its imports).

// ===================================================================
// Options

// Each of the definitions above may have "options" attached.  These are
// just annotations which may cause code to be generated slightly differently
// or may contain hints for code that manipulates protocol messages.
//
// Clients may define custom options as extensions of the *Options messages.
// These extensions may not yet be known at parsing time, so the parser cannot
// store the values in them.  Instead it stores them in a field in the *Options
// message called uninterpreted_option. This field must have the same name
// across all *Options messages. We then use this field to populate the
// extensions when we build a descriptor, at which point all protos have been
// parsed and so all extensions are known.
//
// Extension numbers for custom options may be chosen as follows:
// * For options which will only be used within a single application or
//   organization, or for experimental options, use field numbers 50000
//   through 99999.  It is up to you to ensure that you do not use the
//   same number for multiple options.
// * For options which will be published and used publicly by multiple
//   independent entities, e-mail protobuf-global-extension-registry@google.com
//   to reserve extension numbers. Simply provide your project name (e.g.
//   Objective-C plugin) and your project website (if available) -- there's no
//   need to explain how you intend to use them. Usually you only need one
//   extension number. You can declare multiple options with only one extension
//   number by putting them in a sub-message. See the Custom Options section of
//   the docs for examples:
//   https://developers.google.com/protocol-buffers/docs/proto#options
//   If this turns out to be popular, a web service will be set up
//   to automatically assign option numbers.

// ===================================================================
// Optional source code info
package descriptor

// The protocol compiler can output a FileDescriptorSet containing the .proto
// files it parses.
FileDescriptorSet: {
	file?: [...FileDescriptorProto] @protobuf(1)
}

// Describes a complete .proto file.
FileDescriptorProto: {
	name?:    string @protobuf(1) // file name, relative to root of source tree
	package?: string @protobuf(2) // e.g. "foo", "foo.bar", etc.

	// Names of files imported by this file.
	dependency?: [...string] @protobuf(3)

	// Indexes of the public imported files in the dependency list above.
	publicDependency?: [...int32] @protobuf(10,name=public_dependency)

	// Indexes of the weak imported files in the dependency list.
	// For Google-internal migration only. Do not use.
	weakDependency?: [...int32] @protobuf(11,name=weak_dependency)

	// All top-level definitions in this file.
	messageType?: [...DescriptorProto] @protobuf(4,name=message_type)
	enumType?: [...EnumDescriptorProto] @protobuf(5,name=enum_type)
	service?: [...ServiceDescriptorProto] @protobuf(6)
	extension?: [...FieldDescriptorProto] @protobuf(7)
	options?: FileOptions @protobuf(8)

	// This field contains optional information about the original source code.
	// You may safely remove this entire field without harming runtime
	// functionality of the descriptors -- the information is needed only by
	// development tools.
	sourceCodeInfo?: SourceCodeInfo @protobuf(9,name=source_code_info)

	// The syntax of the proto file.
	// The supported values are "proto2" and "proto3".
	syntax?: string @protobuf(12)
}

// Describes a message type.
DescriptorProto: {
	name?: string @protobuf(1)
	field?: [...FieldDescriptorProto] @protobuf(2)
	extension?: [...FieldDescriptorProto] @protobuf(6)
	nestedType?: [...DescriptorProto] @protobuf(3,name=nested_type)
	enumType?: [...EnumDescriptorProto] @protobuf(4,name=enum_type)
	extensionRange?: [...DescriptorProto_ExtensionRange] @protobuf(5,type=ExtensionRange,name=extension_range)
	oneofDecl?: [...OneofDescriptorProto] @protobuf(8,name=oneof_decl)
	options?: MessageOptions @protobuf(7)
	reservedRange?: [...DescriptorProto_ReservedRange] @protobuf(9,type=ReservedRange,name=reserved_range)

	// Reserved field names, which may not be used by fields in the same message.
	// A given name may only be reserved once.
	reservedName?: [...string] @protobuf(10,name=reserved_name)
}

DescriptorProto_ExtensionRange: {
	start?:   int32                 @protobuf(1) // Inclusive.
	end?:     int32                 @protobuf(2) // Exclusive.
	options?: ExtensionRangeOptions @protobuf(3)
}

// Range of reserved tag numbers. Reserved tag numbers may not be used by
// fields or extension ranges in the same message. Reserved ranges may
// not overlap.
DescriptorProto_ReservedRange: {
	start?: int32 @protobuf(1) // Inclusive.
	end?:   int32 @protobuf(2) // Exclusive.
}

ExtensionRangeOptions: {
	// The parser stores options it doesn't recognize here. See above.
	uninterpretedOption?: [...UninterpretedOption] @protobuf(999,name=uninterpreted_option)
}

// Describes a field within a message.
FieldDescriptorProto: {
	name?:   string                     @protobuf(1)
	number?: int32                      @protobuf(3)
	label?:  FieldDescriptorProto_Label @protobuf(4,type=Label)

	// If type_name is set, this need not be set.  If both this and type_name
	// are set, this must be one of TYPE_ENUM, TYPE_MESSAGE or TYPE_GROUP.
	type?: FieldDescriptorProto_Type @protobuf(5,type=Type)

	// For message and enum types, this is the name of the type.  If the name
	// starts with a '.', it is fully-qualified.  Otherwise, C++-like scoping
	// rules are used to find the type (i.e. first the nested types within this
	// message are searched, then within the parent, on up to the root
	// namespace).
	typeName?: string @protobuf(6,name=type_name)

	// For extensions, this is the name of the type being extended.  It is
	// resolved in the same manner as type_name.
	extendee?: string @protobuf(2)

	// For numeric types, contains the original text representation of the value.
	// For booleans, "true" or "false".
	// For strings, contains the default text contents (not escaped in any way).
	// For bytes, contains the C escaped value.  All bytes >= 128 are escaped.
	// TODO(kenton):  Base-64 encode?
	defaultValue?: string @protobuf(7,name=default_value)

	// If set, gives the index of a oneof in the containing type's oneof_decl
	// list.  This field is a member of that oneof.
	oneofIndex?: int32 @protobuf(9,name=oneof_index)

	// JSON name of this field. The value is set by protocol compiler. If the
	// user has set a "json_name" option on this field, that option's value
	// will be used. Otherwise, it's deduced from the field's name by converting
	// it to camelCase.
	jsonName?: string       @protobuf(10,name=json_name)
	options?:  FieldOptions @protobuf(8)
}
FieldDescriptorProto_Type:
	// 0 is reserved for errors.
	// Order is weird for historical reasons.
	"TYPE_DOUBLE" |
	"TYPE_FLOAT" |

	// Not ZigZag encoded.  Negative numbers take 10 bytes.  Use TYPE_SINT64 if
	// negative values are likely.
	"TYPE_INT64" |
	"TYPE_UINT64" |

	// Not ZigZag encoded.  Negative numbers take 10 bytes.  Use TYPE_SINT32 if
	// negative values are likely.
	"TYPE_INT32" |
	"TYPE_FIXED64" |
	"TYPE_FIXED32" |
	"TYPE_BOOL" |
	"TYPE_STRING" |

	// Tag-delimited aggregate.
	// Group type is deprecated and not supported in proto3. However, Proto3
	// implementations should still be able to parse the group wire format and
	// treat group fields as unknown fields.
	"TYPE_GROUP" |
	"TYPE_MESSAGE" | // Length-delimited aggregate.

	// New in version 2.
	"TYPE_BYTES" |
	"TYPE_UINT32" |
	"TYPE_ENUM" |
	"TYPE_SFIXED32" |
	"TYPE_SFIXED64" |
	"TYPE_SINT32" | // Uses ZigZag encoding.
	"TYPE_SINT64" // Uses ZigZag encoding.

FieldDescriptorProto_Type_value: {
	"TYPE_DOUBLE":   1
	"TYPE_FLOAT":    2
	"TYPE_INT64":    3
	"TYPE_UINT64":   4
	"TYPE_INT32":    5
	"TYPE_FIXED64":  6
	"TYPE_FIXED32":  7
	"TYPE_BOOL":     8
	"TYPE_STRING":   9
	"TYPE_GROUP":    10
	"TYPE_MESSAGE":  11
	"TYPE_BYTES":    12
	"TYPE_UINT32":   13
	"TYPE_ENUM":     14
	"TYPE_SFIXED32": 15
	"TYPE_SFIXED64": 16
	"TYPE_SINT32":   17
	"TYPE_SINT64":   18
}
FieldDescriptorProto_Label:
	// 0 is reserved for errors
	"LABEL_OPTIONAL" |
	"LABEL_REQUIRED" |
	"LABEL_REPEATED"

FieldDescriptorProto_Label_value: {
	"LABEL_OPTIONAL": 1
	"LABEL_REQUIRED": 2
	"LABEL_REPEATED": 3
}

// Describes a oneof.
OneofDescriptorProto: {
	name?:    string       @protobuf(1)
	options?: OneofOptions @protobuf(2)
}

// Describes an enum type.
EnumDescriptorProto: {
	name?: string @protobuf(1)
	value?: [...EnumValueDescriptorProto] @protobuf(2)
	options?: EnumOptions @protobuf(3)

	// Range of reserved numeric values. Reserved numeric values may not be used
	// by enum values in the same enum declaration. Reserved ranges may not
	// overlap.
	reservedRange?: [...EnumDescriptorProto_EnumReservedRange] @protobuf(4,type=EnumReservedRange,name=reserved_range)

	// Reserved enum value names, which may not be reused. A given name may only
	// be reserved once.
	reservedName?: [...string] @protobuf(5,name=reserved_name)
}

// Range of reserved numeric values. Reserved values may not be used by
// entries in the same enum. Reserved ranges may not overlap.
//
// Note that this is distinct from DescriptorProto.ReservedRange in that it
// is inclusive such that it can appropriately represent the entire int32
// domain.
EnumDescriptorProto_EnumReservedRange: {
	start?: int32 @protobuf(1) // Inclusive.
	end?:   int32 @protobuf(2) // Inclusive.
}

// Describes a value within an enum.
EnumValueDescriptorProto: {
	name?:    string           @protobuf(1)
	number?:  int32            @protobuf(2)
	options?: EnumValueOptions @protobuf(3)
}

// Describes a service.
ServiceDescriptorProto: {
	name?: string @protobuf(1)
	method?: [...MethodDescriptorProto] @protobuf(2)
	options?: ServiceOptions @protobuf(3)
}

// Describes a method of a service.
MethodDescriptorProto: {
	name?: string @protobuf(1)

	// Input and output type names.  These are resolved in the same way as
	// FieldDescriptorProto.type_name, but must refer to a message type.
	inputType?:  string        @protobuf(2,name=input_type)
	outputType?: string        @protobuf(3,name=output_type)
	options?:    MethodOptions @protobuf(4)

	// Identifies if client streams multiple client messages
	clientStreaming?: bool @protobuf(5,name=client_streaming,"default=false")

	// Identifies if server streams multiple server messages
	serverStreaming?: bool @protobuf(6,name=server_streaming,"default=false")
}

FileOptions: {
	// Sets the Java package where classes generated from this .proto will be
	// placed.  By default, the proto package is used, but this is often
	// inappropriate because proto packages do not normally start with backwards
	// domain names.
	javaPackage?: string @protobuf(1,name=java_package)

	// If set, all the classes from the .proto file are wrapped in a single
	// outer class with the given name.  This applies to both Proto1
	// (equivalent to the old "--one_java_file" option) and Proto2 (where
	// a .proto always translates to a single class, but you may want to
	// explicitly choose the class name).
	javaOuterClassname?: string @protobuf(8,name=java_outer_classname)

	// If set true, then the Java code generator will generate a separate .java
	// file for each top-level message, enum, and service defined in the .proto
	// file.  Thus, these types will *not* be nested inside the outer class
	// named by java_outer_classname.  However, the outer class will still be
	// generated to contain the file's getDescriptor() method as well as any
	// top-level extensions defined in the file.
	javaMultipleFiles?: bool @protobuf(10,name=java_multiple_files,"default=false")

	// This option does nothing.
	javaGenerateEqualsAndHash?: bool @protobuf(20,name=java_generate_equals_and_hash,deprecated)

	// If set true, then the Java2 code generator will generate code that
	// throws an exception whenever an attempt is made to assign a non-UTF-8
	// byte sequence to a string field.
	// Message reflection will do the same.
	// However, an extension field still accepts non-UTF-8 byte sequences.
	// This option has no effect on when used with the lite runtime.
	javaStringCheckUtf8?: bool                     @protobuf(27,name=java_string_check_utf8,"default=false")
	optimizeFor?:         FileOptions_OptimizeMode @protobuf(9,type=OptimizeMode,name=optimize_for,"default=SPEED")

	// Sets the Go package where structs generated from this .proto will be
	// placed. If omitted, the Go package will be derived from the following:
	//   - The basename of the package import path, if provided.
	//   - Otherwise, the package statement in the .proto file, if present.
	//   - Otherwise, the basename of the .proto file, without extension.
	goPackage?: string @protobuf(11,name=go_package)

	// Should generic services be generated in each language?  "Generic" services
	// are not specific to any particular RPC system.  They are generated by the
	// main code generators in each language (without additional plugins).
	// Generic services were the only kind of service generation supported by
	// early versions of google.protobuf.
	//
	// Generic services are now considered deprecated in favor of using plugins
	// that generate code specific to your particular RPC system.  Therefore,
	// these default to false.  Old code which depends on generic services should
	// explicitly set them to true.
	ccGenericServices?:   bool @protobuf(16,name=cc_generic_services,"default=false")
	javaGenericServices?: bool @protobuf(17,name=java_generic_services,"default=false")
	pyGenericServices?:   bool @protobuf(18,name=py_generic_services,"default=false")
	phpGenericServices?:  bool @protobuf(42,name=php_generic_services,"default=false")

	// Is this file deprecated?
	// Depending on the target platform, this can emit Deprecated annotations
	// for everything in the file, or it will be completely ignored; in the very
	// least, this is a formalization for deprecating files.
	deprecated?: bool @protobuf(23,"default=false")

	// Enables the use of arenas for the proto messages in this file. This applies
	// only to generated classes for C++.
	ccEnableArenas?: bool @protobuf(31,name=cc_enable_arenas,"default=false")

	// Sets the objective c class prefix which is prepended to all objective c
	// generated classes from this .proto. There is no default.
	objcClassPrefix?: string @protobuf(36,name=objc_class_prefix)

	// Namespace for generated classes; defaults to the package.
	csharpNamespace?: string @protobuf(37,name=csharp_namespace)

	// By default Swift generators will take the proto package and CamelCase it
	// replacing '.' with underscore and use that to prefix the types/symbols
	// defined. When this options is provided, they will use this value instead
	// to prefix the types/symbols defined.
	swiftPrefix?: string @protobuf(39,name=swift_prefix)

	// Sets the php class prefix which is prepended to all php generated classes
	// from this .proto. Default is empty.
	phpClassPrefix?: string @protobuf(40,name=php_class_prefix)

	// Use this option to change the namespace of php generated classes. Default
	// is empty. When this option is empty, the package name will be used for
	// determining the namespace.
	phpNamespace?: string @protobuf(41,name=php_namespace)

	// Use this option to change the namespace of php generated metadata classes.
	// Default is empty. When this option is empty, the proto file name will be
	// used for determining the namespace.
	phpMetadataNamespace?: string @protobuf(44,name=php_metadata_namespace)

	// Use this option to change the package of ruby generated classes. Default
	// is empty. When this option is not set, the package name will be used for
	// determining the ruby package.
	rubyPackage?: string @protobuf(45,name=ruby_package)

	// The parser stores options it doesn't recognize here.
	// See the documentation for the "Options" section above.
	uninterpretedOption?: [...UninterpretedOption] @protobuf(999,name=uninterpreted_option)
}

// Generated classes can be optimized for speed or code size.
FileOptions_OptimizeMode: "SPEED" | // Generate complete code for parsing, serialization,

	// etc.
	"CODE_SIZE" |
	"LITE_RUNTIME" // Generate code using MessageLite and the lite runtime.

FileOptions_OptimizeMode_value: {
	"SPEED":        1
	"CODE_SIZE":    2 // Use ReflectionOps to implement these methods.
	"LITE_RUNTIME": 3
}

MessageOptions: {
	// Set true to use the old proto1 MessageSet wire format for extensions.
	// This is provided for backwards-compatibility with the MessageSet wire
	// format.  You should not use this for any other reason:  It's less
	// efficient, has fewer features, and is more complicated.
	//
	// The message must be defined exactly as follows:
	//   message Foo {
	//     option message_set_wire_format = true;
	//     extensions 4 to max;
	//   }
	// Note that the message cannot have any defined fields; MessageSets only
	// have extensions.
	//
	// All extensions of your type must be singular messages; e.g. they cannot
	// be int32s, enums, or repeated messages.
	//
	// Because this is an option, the above two restrictions are not enforced by
	// the protocol compiler.
	messageSetWireFormat?: bool @protobuf(1,name=message_set_wire_format,"default=false")

	// Disables the generation of the standard "descriptor()" accessor, which can
	// conflict with a field of the same name.  This is meant to make migration
	// from proto1 easier; new code should avoid fields named "descriptor".
	noStandardDescriptorAccessor?: bool @protobuf(2,name=no_standard_descriptor_accessor,"default=false")

	// Is this message deprecated?
	// Depending on the target platform, this can emit Deprecated annotations
	// for the message, or it will be completely ignored; in the very least,
	// this is a formalization for deprecating messages.
	deprecated?: bool @protobuf(3,"default=false")

	// Whether the message is an automatically generated map entry type for the
	// maps field.
	//
	// For maps fields:
	//     map<KeyType, ValueType> map_field = 1;
	// The parsed descriptor looks like:
	//     message MapFieldEntry {
	//         option map_entry = true;
	//         optional KeyType key = 1;
	//         optional ValueType value = 2;
	//     }
	//     repeated MapFieldEntry map_field = 1;
	//
	// Implementations may choose not to generate the map_entry=true message, but
	// use a native map in the target language to hold the keys and values.
	// The reflection APIs in such implementations still need to work as
	// if the field is a repeated message field.
	//
	// NOTE: Do not set the option in .proto files. Always use the maps syntax
	// instead. The option should only be implicitly set by the proto compiler
	// parser.
	mapEntry?: bool @protobuf(7,name=map_entry)

	// The parser stores options it doesn't recognize here. See above.
	uninterpretedOption?: [...UninterpretedOption] @protobuf(999,name=uninterpreted_option)
}

FieldOptions: {
	// The ctype option instructs the C++ code generator to use a different
	// representation of the field than it normally would.  See the specific
	// options below.  This option is not yet implemented in the open source
	// release -- sorry, we'll try to include it in a future version!
	ctype?: FieldOptions_CType @protobuf(1,type=CType,"default=STRING")

	// The packed option can be enabled for repeated primitive fields to enable
	// a more efficient representation on the wire. Rather than repeatedly
	// writing the tag and type for each element, the entire array is encoded as
	// a single length-delimited blob. In proto3, only explicit setting it to
	// false will avoid using packed encoding.
	packed?: bool @protobuf(2)

	// The jstype option determines the JavaScript type used for values of the
	// field.  The option is permitted only for 64 bit integral and fixed types
	// (int64, uint64, sint64, fixed64, sfixed64).  A field with jstype JS_STRING
	// is represented as JavaScript string, which avoids loss of precision that
	// can happen when a large value is converted to a floating point JavaScript.
	// Specifying JS_NUMBER for the jstype causes the generated JavaScript code to
	// use the JavaScript "number" type.  The behavior of the default option
	// JS_NORMAL is implementation dependent.
	//
	// This option is an enum to permit additional types to be added, e.g.
	// goog.math.Integer.
	jstype?: FieldOptions_JSType @protobuf(6,type=JSType,"default=JS_NORMAL")

	// Should this field be parsed lazily?  Lazy applies only to message-type
	// fields.  It means that when the outer message is initially parsed, the
	// inner message's contents will not be parsed but instead stored in encoded
	// form.  The inner message will actually be parsed when it is first accessed.
	//
	// This is only a hint.  Implementations are free to choose whether to use
	// eager or lazy parsing regardless of the value of this option.  However,
	// setting this option true suggests that the protocol author believes that
	// using lazy parsing on this field is worth the additional bookkeeping
	// overhead typically needed to implement it.
	//
	// This option does not affect the public interface of any generated code;
	// all method signatures remain the same.  Furthermore, thread-safety of the
	// interface is not affected by this option; const methods remain safe to
	// call from multiple threads concurrently, while non-const methods continue
	// to require exclusive access.
	//
	//
	// Note that implementations may choose not to check required fields within
	// a lazy sub-message.  That is, calling IsInitialized() on the outer message
	// may return true even if the inner message has missing required fields.
	// This is necessary because otherwise the inner message would have to be
	// parsed in order to perform the check, defeating the purpose of lazy
	// parsing.  An implementation which chooses not to check required fields
	// must be consistent about it.  That is, for any particular sub-message, the
	// implementation must either *always* check its required fields, or *never*
	// check its required fields, regardless of whether or not the message has
	// been parsed.
	lazy?: bool @protobuf(5,"default=false")

	// Is this field deprecated?
	// Depending on the target platform, this can emit Deprecated annotations
	// for accessors, or it will be completely ignored; in the very least, this
	// is a formalization for deprecating fields.
	deprecated?: bool @protobuf(3,"default=false")

	// For Google-internal migration only. Do not use.
	weak?: bool @protobuf(10,"default=false")

	// The parser stores options it doesn't recognize here. See above.
	uninterpretedOption?: [...UninterpretedOption] @protobuf(999,name=uninterpreted_option)
}
FieldOptions_CType:
	// Default mode.
	"STRING" |
	"CORD" |
	"STRING_PIECE"

FieldOptions_CType_value: {
	"STRING":       0
	"CORD":         1
	"STRING_PIECE": 2
}
FieldOptions_JSType:
	// Use the default type.
	"JS_NORMAL" |

	// Use JavaScript strings.
	"JS_STRING" |

	// Use JavaScript numbers.
	"JS_NUMBER"

FieldOptions_JSType_value: {
	"JS_NORMAL": 0
	"JS_STRING": 1
	"JS_NUMBER": 2
}

OneofOptions: {
	// The parser stores options it doesn't recognize here. See above.
	uninterpretedOption?: [...UninterpretedOption] @protobuf(999,name=uninterpreted_option)
}

EnumOptions: {
	// Set this option to true to allow mapping different tag names to the same
	// value.
	allowAlias?: bool @protobuf(2,name=allow_alias)

	// Is this enum deprecated?
	// Depending on the target platform, this can emit Deprecated annotations
	// for the enum, or it will be completely ignored; in the very least, this
	// is a formalization for deprecating enums.
	deprecated?: bool @protobuf(3,"default=false")

	// The parser stores options it doesn't recognize here. See above.
	uninterpretedOption?: [...UninterpretedOption] @protobuf(999,name=uninterpreted_option)
}

EnumValueOptions: {
	// Is this enum value deprecated?
	// Depending on the target platform, this can emit Deprecated annotations
	// for the enum value, or it will be completely ignored; in the very least,
	// this is a formalization for deprecating enum values.
	deprecated?: bool @protobuf(1,"default=false")

	// The parser stores options it doesn't recognize here. See above.
	uninterpretedOption?: [...UninterpretedOption] @protobuf(999,name=uninterpreted_option)
}

ServiceOptions: {

	// Note:  Field numbers 1 through 32 are reserved for Google's internal RPC
	//   framework.  We apologize for hoarding these numbers to ourselves, but
	//   we were already using them long before we decided to release Protocol
	//   Buffers.

	// Is this service deprecated?
	// Depending on the target platform, this can emit Deprecated annotations
	// for the service, or it will be completely ignored; in the very least,
	// this is a formalization for deprecating services.
	deprecated?: bool @protobuf(33,"default=false")

	// The parser stores options it doesn't recognize here. See above.
	uninterpretedOption?: [...UninterpretedOption] @protobuf(999,name=uninterpreted_option)
}

MethodOptions: {

	// Note:  Field numbers 1 through 32 are reserved for Google's internal RPC
	//   framework.  We apologize for hoarding these numbers to ourselves, but
	//   we were already using them long before we decided to release Protocol
	//   Buffers.

	// Is this method deprecated?
	// Depending on the target platform, this can emit Deprecated annotations
	// for the method, or it will be completely ignored; in the very least,
	// this is a formalization for deprecating methods.
	deprecated?:       bool                           @protobuf(33,"default=false")
	idempotencyLevel?: MethodOptions_IdempotencyLevel @protobuf(34,type=IdempotencyLevel,name=idempotency_level,"default=IDEMPOTENCY_UNKNOWN")

	// The parser stores options it doesn't recognize here. See above.
	uninterpretedOption?: [...UninterpretedOption] @protobuf(999,name=uninterpreted_option)
}

// Is this method side-effect-free (or safe in HTTP parlance), or idempotent,
// or neither? HTTP based RPC implementation may choose GET verb for safe
// methods, and PUT verb for idempotent methods instead of the default POST.
MethodOptions_IdempotencyLevel: "IDEMPOTENCY_UNKNOWN" |
	"NO_SIDE_EFFECTS" | // implies idempotent
	"IDEMPOTENT" // idempotent, but may have side effects

MethodOptions_IdempotencyLevel_value: {
	"IDEMPOTENCY_UNKNOWN": 0
	"NO_SIDE_EFFECTS":     1
	"IDEMPOTENT":          2
}

// A message representing a option the parser does not recognize. This only
// appears in options protos created by the compiler::Parser class.
// DescriptorPool resolves these when building Descriptor objects. Therefore,
// options protos in descriptor objects (e.g. returned by Descriptor::options(),
// or produced by Descriptor::CopyTo()) will never have UninterpretedOptions
// in them.
UninterpretedOption: {
	name?: [...UninterpretedOption_NamePart] @protobuf(2,type=NamePart)

	// The value of the uninterpreted option, in whatever type the tokenizer
	// identified it as during parsing. Exactly one of these should be set.
	identifierValue?:  string  @protobuf(3,name=identifier_value)
	positiveIntValue?: uint64  @protobuf(4,name=positive_int_value)
	negativeIntValue?: int64   @protobuf(5,name=negative_int_value)
	doubleValue?:      float64 @protobuf(6,type=double,name=double_value)
	stringValue?:      bytes   @protobuf(7,name=string_value)
	aggregateValue?:   string  @protobuf(8,name=aggregate_value)
}

// The name of the uninterpreted option.  Each string represents a segment in
// a dot-separated name.  is_extension is true iff a segment represents an
// extension (denoted with parentheses in options specs in .proto files).
// E.g.,{ ["foo", false], ["bar.baz", true], ["qux", false] } represents
// "foo.(bar.baz).qux".
UninterpretedOption_NamePart: {
	namePart?:    string @protobuf(1,name=name_part)
	isExtension?: bool   @protobuf(2,name=is_extension)
}

// Encapsulates information about the original source file from which a
// FileDescriptorProto was generated.
SourceCodeInfo: {
	// A Location identifies a piece of source code in a .proto file which
	// corresponds to a particular definition.  This information is intended
	// to be useful to IDEs, code indexers, documentation generators, and similar
	// tools.
	//
	// For example, say we have a file like:
	//   message Foo {
	//     optional string foo = 1;
	//   }
	// Let's look at just the field definition:
	//   optional string foo = 1;
	//   ^       ^^     ^^  ^  ^^^
	//   a       bc     de  f  ghi
	// We have the following locations:
	//   span   path               represents
	//   [a,i)  [ 4, 0, 2, 0 ]     The whole field definition.
	//   [a,b)  [ 4, 0, 2, 0, 4 ]  The label (optional).
	//   [c,d)  [ 4, 0, 2, 0, 5 ]  The type (string).
	//   [e,f)  [ 4, 0, 2, 0, 1 ]  The name (foo).
	//   [g,h)  [ 4, 0, 2, 0, 3 ]  The number (1).
	//
	// Notes:
	// - A location may refer to a repeated field itself (i.e. not to any
	//   particular index within it).  This is used whenever a set of elements are
	//   logically enclosed in a single code segment.  For example, an entire
	//   extend block (possibly containing multiple extension definitions) will
	//   have an outer location whose path refers to the "extensions" repeated
	//   field without an index.
	// - Multiple locations may have the same path.  This happens when a single
	//   logical declaration is spread out across multiple places.  The most
	//   obvious example is the "extend" block again -- there may be multiple
	//   extend blocks in the same scope, each of which will have the same path.
	// - A location's span is not always a subset of its parent's span.  For
	//   example, the "extendee" of an extension declaration appears at the
	//   beginning of the "extend" block and is shared by all extensions within
	//   the block.
	// - Just because a location's span is a subset of some other location's span
	//   does not mean that it is a descendant.  For example, a "group" defines
	//   both a type and a field in a single declaration.  Thus, the locations
	//   corresponding to the type and field and their components will overlap.
	// - Code which tries to interpret locations should probably be designed to
	//   ignore those that it doesn't understand, as more types of locations could
	//   be recorded in the future.
	location?: [...SourceCodeInfo_Location] @protobuf(1,type=Location)
}

SourceCodeInfo_Location: {
	// Identifies which part of the FileDescriptorProto was defined at this
	// location.
	//
	// Each element is a field number or an index.  They form a path from
	// the root FileDescriptorProto to the place where the definition.  For
	// example, this path:
	//   [ 4, 3, 2, 7, 1 ]
	// refers to:
	//   file.message_type(3)  // 4, 3
	//       .field(7)         // 2, 7
	//       .name()           // 1
	// This is because FileDescriptorProto.message_type has field number 4:
	//   repeated DescriptorProto message_type = 4;
	// and DescriptorProto.field has field number 2:
	//   repeated FieldDescriptorProto field = 2;
	// and FieldDescriptorProto.name has field number 1:
	//   optional string name = 1;
	//
	// Thus, the above path gives the location of a field name.  If we removed
	// the last element:
	//   [ 4, 3, 2, 7 ]
	// this path refers to the whole field declaration (from the beginning
	// of the label to the terminating semicolon).
	path?: [...int32] @protobuf(1,packed)

	// Always has exactly three or four elements: start line, start column,
	// end line (optional, otherwise assumed same as start line), end column.
	// These are packed into a single field for efficiency.  Note that line
	// and column numbers are zero-based -- typically you will want to add
	// 1 to each before displaying to a user.
	span?: [...int32] @protobuf(2,packed)

	// If this SourceCodeInfo represents a complete declaration, these are any
	// comments appearing before and after the declaration which appear to be
	// attached to the declaration.
	//
	// A series of line comments appearing on consecutive lines, with no other
	// tokens appearing on those lines, will be treated as a single comment.
	//
	// leading_detached_comments will keep paragraphs of comments that appear
	// before (but not connected to) the current element. Each paragraph,
	// separated by empty lines, will be one comment element in the repeated
	// field.
	//
	// Only the comment content is provided; comment markers (e.g. //) are
	// stripped out.  For block comments, leading whitespace and an asterisk
	// will be stripped from the beginning of each line other than the first.
	// Newlines are included in the output.
	//
	// Examples:
	//
	//   optional int32 foo = 1;  // Comment attached to foo.
	//   // Comment attached to bar.
	//   optional int32 bar = 2;
	//
	//   optional string baz = 3;
	//   // Comment attached to baz.
	//   // Another line attached to baz.
	//
	//   // Comment attached to qux.
	//   //
	//   // Another line attached to qux.
	//   optional double qux = 4;
	//
	//   // Detached comment for corge. This is not leading or trailing comments
	//   // to qux or corge because there are blank lines separating it from
	//   // both.
	//
	//   // Detached comment for corge paragraph 2.
	//
	//   optional string corge = 5;
	//   /* Block comment attached
	//    * to corge.  Leading asterisks
	//    * will be removed. */
	//   /* Block comment attached to
	//    * grault. */
	//   optional int32 grault = 6;
	//
	//   // ignored detached comments.
	leadingComments?:  string @protobuf(3,name=leading_comments)
	trailingComments?: string @protobuf(4,name=trailing_comments)
	leadingDetachedComments?: [...string] @protobuf(6,name=leading_detached_comments)
}

// Describes the relationship between generated code and its original source
// file. A GeneratedCodeInfo message is associated with only one generated
// source file, but may contain references to different source .proto files.
GeneratedCodeInfo: {
	// An Annotation connects some span of text in generated code to an element
	// of its generating .proto file.
	annotation?: [...GeneratedCodeInfo_Annotation] @protobuf(1,type=Annotation)
}

GeneratedCodeInfo_Annotation: {
	// Identifies the element in the original source .proto file. This field
	// is formatted the same as SourceCodeInfo.Location.path.
	path?: [...int32] @protobuf(1,packed)

	// Identifies the filesystem path to the original source .proto.
	sourceFile?: string @protobuf(2,name=source_file)

	// Identifies the starting offset in bytes in the generated code
	// that relates to the identified object.
	begin?: int32 @protobuf(3)

	// Identifies the ending offset in bytes in the generated code that
	// relates to the identified offset. The end offset should be one past
	// the last relevant byte (so the length of the text = end - begin).
	end?: int32 @protobuf(4)
}
