import "time"

Attributes: {
	//  A map of attribute name to its value.
	attributes: {
		<_>: AttrValue
	}
}

//  The attribute value.
AttrValue: {}

AttrValue: {
	//  Used for values of type STRING, DNS_NAME, EMAIL_ADDRESS, and URI
	stringValue: string @protobuf(2,name=string_value)
} | {
	//  Used for values of type INT64
	int64Value: int64 @protobuf(3,name=int64_value)
} | {
	//  Used for values of type DOUBLE
	doubleValue: float64 @protobuf(4,type=double,name=double_value)
} | {
	//  Used for values of type BOOL
	boolValue: bool @protobuf(5,name=bool_value)
} | {
	//  Used for values of type BYTES
	bytesValue: bytes @protobuf(6,name=bytes_value)
} | {
	//  Used for values of type TIMESTAMP
	timestampValue: time.Time @protobuf(7,type=google.protobuf.Timestamp,name=timestamp_value)
} | {
	//  Used for values of type DURATION
	durationValue: time.Duration @protobuf(8,type=google.protobuf.Duration,name=duration_value)
} | {
	//  Used for values of type STRING_MAP
	stringMapValue: Attributes_StringMap @protobuf(9,type=StringMap,name=string_map_value)
}

Attributes_StringMap: {
	//  Holds a set of name/value pairs.
	entries: {
		<_>: string
	} @protobuf(1,type=map<string,string>)
}
