package full_references

#FullReferenceMsg: {
	nestedMsg?: #FullReferenceNestedMsg @protobuf(1,istio.io.api.other.full_references.FullReferenceNestedMsg,name=nested_msg)
}

#FullReferenceNestedMsg: {

	#FullReferenceDoubleNestedMsg: {
		value?: string @protobuf(1,string)
	}
	nestedMsg?: #FullReferenceNestedMsg.#FullReferenceDoubleNestedMsg @protobuf(1,istio.io.api.other.full_references.FullReferenceNestedMsg.FullReferenceDoubleNestedMsg,name=nested_msg)
}
