// Copyright 2017 Istio Authors
//
//   Licensed under the Apache License, Version 2.0 (the "License");
//   you may not use this file except in compliance with the License.
//   You may obtain a copy of the License at
//
//       http://www.apache.org/licenses/LICENSE-2.0
//
//   Unless required by applicable law or agreed to in writing, software
//   distributed under the License is distributed on an "AS IS" BASIS,
//   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//   See the License for the specific language governing permissions and
//   limitations under the License.

// NOTE: this is a duplicate of proxy.v1.config.IstioService from
// proxy/v1alpha1/config/route_rules.proto.
//
// Mixer protobufs have gogoproto specific options which are not
// compatiable with the proxy's vanilla protobufs. Ideally, these
// protobuf options be reconciled so fundamental Istio concepts and
// types can be shared by components. Until then, make a copy of
// IstioService for mixerclient to use.
package client

// IstioService identifies a service and optionally service version.
// The FQDN of the service is composed from the name, namespace, and implementation-specific domain suffix
// (e.g. on Kubernetes, "reviews" + "default" + "svc.cluster.local" -> "reviews.default.svc.cluster.local").
IstioService: {
	// The short name of the service such as "foo".
	name?: string @protobuf(1)

	// Optional namespace of the service. Defaults to value of metadata namespace field.
	namespace?: string @protobuf(2)

	// Domain suffix used to construct the service FQDN in implementations that support such specification.
	domain?: string @protobuf(3)

	// The service FQDN.
	service?: string @protobuf(4)

	// Optional one or more labels that uniquely identify the service version.
	//
	// *Note:* When used for a VirtualService destination, labels MUST be empty.
	//
	labels?: {
		[string]: string
	} @protobuf(5,type=map<string,string>)
}
