//  Copyright 2017 Istio Authors
// 
//    Licensed under the Apache License, Version 2.0 (the "License");
//    you may not use this file except in compliance with the License.
//    You may obtain a copy of the License at
// 
//        http://www.apache.org/licenses/LICENSE-2.0
// 
//    Unless required by applicable law or agreed to in writing, software
//    distributed under the License is distributed on an "AS IS" BASIS,
//    WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//    See the License for the specific language governing permissions and
//    limitations under the License.

//  Specifies runtime quota rules.
//   * Uses Istio attributes to match individual requests
//   * Specifies list of quotas to use for matched requests.
// 
//  Example1:
//  Charge "request_count" quota with 1 amount for all requests.
// 
//    QuotaSpec:
//      - rules
//        - quotas:
//            quota: request_count
//            charge: 1
// 
//  Example2:
//  For HTTP POST requests with path are prefixed with /books or
//  api.operation is create_books, charge two quotas:
//  * write_count of 1 amount
//  * request_count of 5 amount.
// 
//  ```yaml
//  QuotaSpec:
//    - rules:
//      - match:
//          clause:
//            request.path:
//              string_prefix: /books
//            request.http_method:
//              string_exact: POST
//      - match:
//          clause:
//            api.operation:
//              string_exact: create_books
//      - quotas:
//          quota: write_count
//          charge: 1
//      - quotas:
//          quota: request_count
//          charge: 5
//  ```
package client

//  Determines the quotas used for individual requests.
QuotaSpec: {
	//  A list of Quota rules.
	rules?: [...QuotaRule] @protobuf(1)
}

//  Specifies a rule with list of matches and list of quotas.
//  If any clause matched, the list of quotas will be used.
QuotaRule: {
	//  If empty, match all request.
	//  If any of match is true, it is matched.
	match?: [...AttributeMatch] @protobuf(1)

	//  The list of quotas to charge.
	quotas?: [...Quota] @protobuf(2)
}

//  Describes how to match a given string in HTTP headers. Match is
//  case-sensitive.
StringMatch: {
}
StringMatch: {
	//  exact string match
	exact?: string @protobuf(1)
} | {
	//  prefix-based match
	prefix?: string @protobuf(2)
} | {
	//  ECMAscript style regex-based match
	regex?: string @protobuf(3)
}

//  Specifies a match clause to match Istio attributes
AttributeMatch: {
	//  Map of attribute names to StringMatch type.
	//  Each map element specifies one condition to match.
	// 
	//  Example:
	// 
	//    clause:
	//      source.uid:
	//        exact: SOURCE_UID
	//      request.http_method:
	//        exact: POST
	clause <_>: StringMatch
}

//  Specifies a quota to use with quota name and amount.
Quota: {
	//  The quota name to charge
	quota?: string @protobuf(1)

	//  The quota amount to charge
	charge?: int64 @protobuf(2)
}

//  QuotaSpecBinding defines the binding between QuotaSpecs and one or more
//  IstioService.
QuotaSpecBinding: {
	//  REQUIRED. One or more services to map the listed QuotaSpec onto.
	services?: [...IstioService] @protobuf(1)

	//  REQUIRED. One or more QuotaSpec references that should be mapped to
	//  the specified service(s). The aggregate collection of match
	//  conditions defined in the QuotaSpecs should not overlap.
	quotaSpecs?: [...QuotaSpecBinding_QuotaSpecReference] @protobuf(2,type=QuotaSpecReference,name=quota_specs)
}

//  QuotaSpecReference uniquely identifies the QuotaSpec used in the
//  Binding.
QuotaSpecBinding_QuotaSpecReference: {
	//  REQUIRED. The short name of the QuotaSpec. This is the resource
	//  name defined by the metadata name field.
	name?: string @protobuf(1)

	//  Optional namespace of the QuotaSpec. Defaults to the value of the
	//  metadata namespace field.
	namespace?: string @protobuf(2)
}
