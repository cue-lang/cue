
//  Copyright 2016 Istio Authors
// 
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
// 
//      http://www.apache.org/licenses/LICENSE-2.0
// 
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.
package v1

import (
	"github.com/golang/protobuf/ptypes/duration"
	"github.com/golang/protobuf/ptypes/timestamp"
)

StructWrap: {
	struct?:    {}     @protobuf(1,type=google.protobuf.Struct)
	any?:       _      @protobuf(2,type=google.protobuf.Value)
	listVal?:   [...]  @protobuf(3,type=google.protobuf.ListValue)
	boolVal?:   bool   @protobuf(4,type=google.protobuf.BoolValue)
	stringVal?: string @protobuf(5,type=google.protobuf.StringValue)
	numberVal?: number @protobuf(6,type=google.protobuf.NumberValue)
}

//  Attributes represents a set of typed name/value pairs. Many of Mixer's
//  API either consume and/or return attributes.
// 
//  Istio uses attributes to control the runtime behavior of services running in the service mesh.
//  Attributes are named and typed pieces of metadata describing ingress and egress traffic and the
//  environment this traffic occurs in. An Istio attribute carries a specific piece
//  of information such as the error code of an API request, the latency of an API request, or the
//  original IP address of a TCP connection. For example:
// 
//  ```yaml
//  request.path: xyz/abc
//  request.size: 234
//  request.time: 12:34:56.789 04/17/2017
//  source.ip: 192.168.0.1
//  target.service: example
//  ```
// 
//  A given Istio deployment has a fixed vocabulary of attributes that it understands.
//  The specific vocabulary is determined by the set of attribute producers being used
//  in the deployment. The primary attribute producer in Istio is Envoy, although
//  specialized Mixer adapters and services can also generate attributes.
// 
//  The common baseline set of attributes available in most Istio deployments is defined
//  [here](https://istio.io/docs/reference/config/policy-and-telemetry/attribute-vocabulary/).
// 
//  Attributes are strongly typed. The supported attribute types are defined by
//  [ValueType](https://github.com/istio/api/blob/master/policy/v1beta1/value_type.proto).
//  Each type of value is encoded into one of the so-called transport types present
//  in this message.
// 
//  Defines a map of attributes in uncompressed format.
//  Following places may use this message:
//  1) Configure Istio/Proxy with static per-proxy attributes, such as source.uid.
//  2) Service IDL definition to extract api attributes for active requests.
//  3) Forward attributes from client proxy to server proxy for HTTP requests.
Attributes: {
	//  A map of attribute name to its value.
	attributes <_>: Attributes_AttributeValue
}

//  Specifies one attribute value with different type.
Attributes_AttributeValue: {
}
//  The attribute value.
Attributes_AttributeValue: {
	//  Used for values of type STRING, DNS_NAME, EMAIL_ADDRESS, and URI
	stringValue?: string @protobuf(2,name=string_value)
} | {
	//  Used for values of type INT64
	int64Value?: int64 @protobuf(3,name=int64_value)
} | {
	//  Used for values of type DOUBLE
	doubleValue?: float64 @protobuf(4,type=double,name=double_value)
} | {
	//  Used for values of type BOOL
	boolValue?: bool @protobuf(5,name=bool_value)
} | {
	//  Used for values of type BYTES
	bytesValue?: bytes @protobuf(6,name=bytes_value)
} | {
	//  Used for values of type TIMESTAMP
	timestampValue?: timestamp.Timestamp @protobuf(7,type=google.protobuf.Timestamp,name=timestamp_value)
} | {
	//  Used for values of type DURATION
	durationValue?: duration.Duration @protobuf(8,type=google.protobuf.Duration,name=duration_value)
} | {
	//  Used for values of type STRING_MAP
	stringMapValue?: Attributes_StringMap @protobuf(9,type=StringMap,name=string_map_value)
}

//  Defines a string map.
Attributes_StringMap: {
	//  Holds a set of name/value pairs.
	entries <_>: string
}

//  Defines a list of attributes in compressed format optimized for transport.
//  Within this message, strings are referenced using integer indices into
//  one of two string dictionaries. Positive integers index into the global
//  deployment-wide dictionary, whereas negative integers index into the message-level
//  dictionary instead. The message-level dictionary is carried by the
//  `words` field of this message, the deployment-wide dictionary is determined via
//  configuration.
CompressedAttributes: {
	//  The message-level dictionary.
	words?: [...string] @protobuf(1)

	//  Holds attributes of type STRING, DNS_NAME, EMAIL_ADDRESS, URI
	strings <_>: int32

	//  Holds attributes of type INT64
	int64s <_>: int64

	//  Holds attributes of type DOUBLE
	doubles <_>: float64

	//  Holds attributes of type BOOL
	bools <_>: bool

	//  Holds attributes of type TIMESTAMP
	timestamps <_>: timestamp.Timestamp

	//  Holds attributes of type DURATION
	durations <_>: duration.Duration

	//  Holds attributes of type BYTES
	bytes <_>: bytes

	//  Holds attributes of type STRING_MAP
	stringMaps <_>: StringMap
}

//  A map of string to string. The keys and values in this map are dictionary
//  indices (see the [Attributes][istio.mixer.v1.CompressedAttributes] message for an explanation)
StringMap: {
	//  Holds a set of name/value pairs.
	entries <_>: int32
}
