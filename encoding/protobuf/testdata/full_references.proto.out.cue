package full_references

#FullReferenceMsg: {
	nestedMsg?: #FullReferenceNestedMsg @protobuf(1,istio.io.api.other.full_references.FullReferenceNestedMsg,name=nested_msg)
}

#FullReferenceNestedMsg: {

	#FullReferenceDoubleNestedMsg: {
		value?: string @protobuf(1,string)
	}
	nestedMsg?: #FullReferenceNestedMsg.#FullReferenceDoubleNestedMsg @protobuf(1,istio.io.api.other.full_references.FullReferenceNestedMsg.FullReferenceDoubleNestedMsg,name=nested_msg)

	#InnerContainer: {
		#InnerEnum: {"INNER_ENUM_VALUE_1", #enumValue: 0} |
			{"INNER_ENUM_VALUE_2", #enumValue: 1}

		#InnerEnum_value: {
			INNER_ENUM_VALUE_1: 0
			INNER_ENUM_VALUE_2: 1
		}
	}

	// This is a reference to a nested enum within the InnerContainer message,
	// and should resolve correctly to the InnerEnum type via the relative dotted path.
	relativeReference?: #InnerContainer.#InnerEnum @protobuf(2,InnerContainer.InnerEnum,name=relative_reference)

	// AmbiguousMsg is a message that is ambiguous between a top-level message and a nested message.
	#AmbiguousMsg: {

		#Inner: {
			// nested_field is a field that distinguishes this Inner from the in the top-level AmbiguousMsg message.
			nestedField?: string @protobuf(1,string,name=nested_field)
		}
	}

	// This is a fully-qualified reference to the top-level AmbiguousMsg message.
	// This does not actually work correctly.
	// TODO: Disambiguate this reference in generated CUE once aliasv2 is non-experimental
	// and we can define something like `let <package> = self` without requiring experiment opt-in.
	absoluteOuter?: #AmbiguousMsg @protobuf(3,.AmbiguousMsg,name=absolute_outer)

	// This is a fully-qualified reference to the nested Inner message of the top-level AmbiguousMsg message.
	// This does not actually work correctly.
	// TODO: Disambiguate this reference in generated CUE once aliasv2 is non-experimental
	// and we can define something like `let <package> = self` without requiring experiment opt-in.
	absoluteInner?: #AmbiguousMsg.#Inner @protobuf(4,.AmbiguousMsg.Inner,name=absolute_inner)

	// This is a relative reference to the nested AmbiguousMsg message.
	relativeOuter?: #AmbiguousMsg @protobuf(5,AmbiguousMsg,name=relative_outer)

	// This is a relative reference to the nested Inner message of the nested AmbiguousMsg message.
	relativeInner?: #AmbiguousMsg.#Inner @protobuf(6,AmbiguousMsg.Inner,name=relative_inner)
}

// AmbiguousMsg is a message that is ambiguous between a top-level message and a nested message.
#AmbiguousMsg: {

	#Inner: {
		// top_level_inner is a field that distinguishes this Inner from the one nested in FullReferenceMsg.
		topLevelInner?: string @protobuf(1,string,name=top_level_inner)
	}
}
