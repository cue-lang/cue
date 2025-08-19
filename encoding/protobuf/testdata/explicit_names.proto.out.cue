package explicit_names

#ExplicitMsg: {
	nestedMsg?: #ExplicitNestedMsg @protobuf(1,istio.io.api.other.explicit_names.ExplicitNestedMsg,name=nested_msg)
}

#ExplicitNestedMsg: {
	value?: string @protobuf(1,string)
}
