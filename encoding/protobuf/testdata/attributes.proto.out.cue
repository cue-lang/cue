
// Copyright 2016 Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
package v1

import (
	"googleapis.com/acme/test"
	"googleapis.com/acme/test/test"
	"time"
)

StructWrap: {
	struct?:    {}     @protobuf(1,type=google.protobuf.Struct)
	any?:       _      @protobuf(2,type=google.protobuf.Value)
	listVal?:   [...]  @protobuf(3,type=google.protobuf.ListValue)
	boolVal?:   bool   @protobuf(4,type=google.protobuf.BoolValue)
	stringVal?: string @protobuf(5,type=google.protobuf.StringValue)
	numberVal?: number @protobuf(6,type=google.protobuf.NumberValue)
}

// Attributes represents a set of typed name/value pairs. Many of Mixer's
// API either consume and/or return attributes.
//
// Istio uses attributes to control the runtime behavior of services running in the service mesh.
// Attributes are named and typed pieces of metadata describing ingress and egress traffic and the
// environment this traffic occurs in. An Istio attribute carries a specific piece
// of information such as the error code of an API request, the latency of an API request, or the
// original IP address of a TCP connection. For example:
//
// ```yaml
// request.path: xyz/abc
// request.size: 234
// request.time: 12:34:56.789 04/17/2017
// source.ip: 192.168.0.1
// target.service: example
// ```
//
Attributes: {
	// A map of attribute name to its value.
	attributes: {
		<_>: Attributes_AttributeValue
	} @protobuf(1,type=map<string,AttributeValue>)
}

// Specifies one attribute value with different type.
Attributes_AttributeValue: {
}
// The attribute value.
Attributes_AttributeValue: {
	// Used for values of type STRING, DNS_NAME, EMAIL_ADDRESS, and URI
	stringValue: string @protobuf(2,name=string_value)
} | {
	// Used for values of type INT64
	int64Value: int64 @protobuf(3,name=int64_value)
} | {
	// Used for values of type DOUBLE
	doubleValue: float64 @protobuf(4,type=double,name=double_value)
} | {
	// Used for values of type BOOL
	boolValue: bool @protobuf(5,name=bool_value)
} | {
	// Used for values of type BYTES
	bytesValue: bytes @protobuf(6,name=bytes_value)
} | {
	// Used for values of type TIMESTAMP
	timestampValue: time.Time @protobuf(7,type=google.protobuf.Timestamp,name=timestamp_value)
} | {
	// Used for values of type DURATION
	durationValue: time.Duration @protobuf(8,type=google.protobuf.Duration,name=duration_value)
} | {
	// Used for values of type STRING_MAP
	stringMapValue: Attributes_StringMap @protobuf(9,type=StringMap,name=string_map_value)
} | {
	testValue: test.Test @protobuf(10,type=acme.test.Test,name=test_value)
} | {
	testValue: test_test.AnotherTest @protobuf(11,type=acme.test.test.AnotherTest,name=test_value)
}

// Defines a string map.
Attributes_StringMap: {
	// Holds a set of name/value pairs.
	entries: {
		<_>: string
	} @protobuf(1,type=map<string,string>)
}

// Defines a list of attributes in compressed format optimized for transport.
// Within this message, strings are referenced using integer indices into
// one of two string dictionaries. Positive integers index into the global
// deployment-wide dictionary, whereas negative integers index into the message-level
// dictionary instead. The message-level dictionary is carried by the
// `words` field of this message, the deployment-wide dictionary is determined via
// configuration.
CompressedAttributes: {
	// The message-level dictionary.
	words?: [...string] @protobuf(1)

	// Holds attributes of type STRING, DNS_NAME, EMAIL_ADDRESS, URI
	strings: {
		<_>: int32
	} @protobuf(2,type=map<sint32,sint32>)

	// Holds attributes of type INT64
	int64s: {
		<_>: int64
	} @protobuf(3,type=map<sint32,int64>)

	// Holds attributes of type DOUBLE
	doubles: {
		<_>: float64
	} @protobuf(4,type=map<sint32,double>)

	// Holds attributes of type BOOL
	bools: {
		<_>: bool
	} @protobuf(5,type=map<sint32,bool>)

	// Holds attributes of type TIMESTAMP
	time: {
		<_>: __time.Time
	} @protobuf(6,type=map<sint32,google.protobuf.Timestamp>,"(gogoproto.nullable)=false","(gogoproto.stdtime)")

	// Holds attributes of type DURATION
	durations: {
		<_>: __time.Duration
	} @protobuf(7,type=map<sint32,google.protobuf.Duration>,"(gogoproto.nullable)=false","(gogoproto.stdduration)")

	// Holds attributes of type BYTES
	bytes: {
		<_>: __bytes
	} @protobuf(8,type=map<sint32,bytes>)

	// Holds attributes of type STRING_MAP
	stringMaps: {
		<_>: StringMap
	} @protobuf(9,type=map<sint32,StringMap>,string_maps,"(gogoproto.nullable)=false")
}
__time = time
__bytes = bytes

// A map of string to string. The keys and values in this map are dictionary
// indices (see the [Attributes][istio.mixer.v1.CompressedAttributes] message for an explanation)
StringMap: {
	// Holds a set of name/value pairs.
	entries: {
		<_>: int32
	} @protobuf(1,type=map<sint32,sint32>)
}
