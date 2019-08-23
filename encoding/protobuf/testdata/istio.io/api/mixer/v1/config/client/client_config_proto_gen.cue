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

// $title: Mixer Client
// $description: Configuration state for the Mixer client library.
// $location: https://istio.io/docs/reference/config/policy-and-telemetry/istio.mixer.v1.config.client

// Describes the configuration state for the Mixer client library that's built into Envoy.
package client

import (
	"istio.io/api/mixer/v1"
	"time"
)

// Specifies the behavior when the client is unable to connect to Mixer.
NetworkFailPolicy: {

	// Specifies the behavior when the client is unable to connect to Mixer.
	policy?: NetworkFailPolicy_FailPolicy @protobuf(1,type=FailPolicy)

	// Max retries on transport error.
	maxRetry?: uint32 @protobuf(2,name=max_retry)

	// Base time to wait between retries.  Will be adjusted by exponential
	// backoff and jitter.
	baseRetryWait?: time.Duration @protobuf(3,type=google.protobuf.Duration,name=base_retry_wait)

	// Max time to wait between retries.
	maxRetryWait?: time.Duration @protobuf(4,type=google.protobuf.Duration,name=max_retry_wait)
}

// Example of single-value enum.
NetworkFailPolicy_FailPolicy:
	// If network connection fails, request is allowed and delivered to the
	// service.
	"FAIL_OPEN"

NetworkFailPolicy_FailPolicy_value "FAIL_OPEN": 0

// Defines the per-service client configuration.
ServiceConfig: {
	// If true, do not call Mixer Check.
	disableCheckCalls?: bool @protobuf(1,name=disable_check_calls)

	// If true, do not call Mixer Report.
	disableReportCalls?: bool @protobuf(2,name=disable_report_calls)

	// Send these attributes to Mixer in both Check and Report. This
	// typically includes the "destination.service" attribute.
	// In case of a per-route override, per-route attributes take precedence
	// over the attributes supplied in the client configuration.
	mixerAttributes?: v1.Attributes @protobuf(3,type=Attributes,name=mixer_attributes)

	// HTTP API specifications to generate API attributes.
	httpApiSpec?: [...HTTPAPISpec] @protobuf(4,name=http_api_spec)

	// Quota specifications to generate quota requirements.
	quotaSpec?: [...QuotaSpec] @protobuf(5,name=quota_spec)

	// Specifies the behavior when the client is unable to connect to Mixer.
	// This is the service-level policy. It overrides
	// [mesh-level
	// policy][istio.mixer.v1.config.client.TransportConfig.network_fail_policy].
	networkFailPolicy?: NetworkFailPolicy @protobuf(7,name=network_fail_policy)

	// Default attributes to forward to upstream. This typically
	// includes the "source.ip" and "source.uid" attributes.
	// In case of a per-route override, per-route attributes take precedence
	// over the attributes supplied in the client configuration.
	//
	// Forwarded attributes take precedence over the static Mixer attributes.
	// The full order of application is as follows:
	// 1. static Mixer attributes from the filter config;
	// 2. static Mixer attributes from the route config;
	// 3. forwarded attributes from the source filter config (if any);
	// 4. forwarded attributes from the source route config (if any);
	// 5. derived attributes from the request metadata.
	forwardAttributes?: v1.Attributes @protobuf(8,type=Attributes,name=forward_attributes)
}

// Defines the transport config on how to call Mixer.
TransportConfig: {
	// The flag to disable check cache.
	disableCheckCache?: bool @protobuf(1,name=disable_check_cache)

	// The flag to disable quota cache.
	disableQuotaCache?: bool @protobuf(2,name=disable_quota_cache)

	// The flag to disable report batch.
	disableReportBatch?: bool @protobuf(3,name=disable_report_batch)

	// Specifies the behavior when the client is unable to connect to Mixer.
	// This is the mesh level policy. The default value for policy is FAIL_OPEN.
	networkFailPolicy?: NetworkFailPolicy @protobuf(4,name=network_fail_policy)

	// Specify refresh interval to write Mixer client statistics to Envoy share
	// memory. If not specified, the interval is 10 seconds.
	statsUpdateInterval?: time.Duration @protobuf(5,type=google.protobuf.Duration,name=stats_update_interval)

	// Name of the cluster that will forward check calls to a pool of mixer
	// servers. Defaults to "mixer_server". By using different names for
	// checkCluster and reportCluster, it is possible to have one set of
	// Mixer servers handle check calls, while another set of Mixer servers
	// handle report calls.
	//
	// NOTE: Any value other than the default "mixer_server" will require the
	// Istio Grafana dashboards to be reconfigured to use the new name.
	checkCluster?: string @protobuf(6,name=check_cluster)

	// Name of the cluster that will forward report calls to a pool of mixer
	// servers. Defaults to "mixer_server". By using different names for
	// checkCluster and reportCluster, it is possible to have one set of
	// Mixer servers handle check calls, while another set of Mixer servers
	// handle report calls.
	//
	// NOTE: Any value other than the default "mixer_server" will require the
	// Istio Grafana dashboards to be reconfigured to use the new name.
	reportCluster?: string @protobuf(7,name=report_cluster)

	// Default attributes to forward to Mixer upstream. This typically
	// includes the "source.ip" and "source.uid" attributes. These
	// attributes are consumed by the proxy in front of mixer.
	attributesForMixerProxy?: v1.Attributes @protobuf(8,type=Attributes,name=attributes_for_mixer_proxy)
}

// Defines the client config for HTTP.
HttpClientConfig: {
	// The transport config.
	transport?: TransportConfig @protobuf(1)

	// Map of control configuration indexed by destination.service. This
	// is used to support per-service configuration for cases where a
	// mixerclient serves multiple services.
	serviceConfigs?: {
		<_>: ServiceConfig
	} @protobuf(2,type=map<string,ServiceConfig>,service_configs)

	// Default destination service name if none was specified in the
	// client request.
	defaultDestinationService?: string @protobuf(3,name=default_destination_service)

	// Default attributes to send to Mixer in both Check and
	// Report. This typically includes "destination.ip" and
	// "destination.uid" attributes.
	mixerAttributes?: v1.Attributes @protobuf(4,type=Attributes,name=mixer_attributes)

	// Default attributes to forward to upstream. This typically
	// includes the "source.ip" and "source.uid" attributes.
	forwardAttributes?: v1.Attributes @protobuf(5,type=Attributes,name=forward_attributes)
}

// Defines the client config for TCP.
TcpClientConfig: {
	// The transport config.
	transport?: TransportConfig @protobuf(1)

	// Default attributes to send to Mixer in both Check and
	// Report. This typically includes "destination.ip" and
	// "destination.uid" attributes.
	mixerAttributes?: v1.Attributes @protobuf(2,type=Attributes,name=mixer_attributes)

	// If set to true, disables Mixer check calls.
	disableCheckCalls?: bool @protobuf(3,name=disable_check_calls)

	// If set to true, disables Mixer check calls.
	disableReportCalls?: bool @protobuf(4,name=disable_report_calls)

	// Quota specifications to generate quota requirements.
	// It applies on the new TCP connections.
	connectionQuotaSpec?: QuotaSpec @protobuf(5,name=connection_quota_spec)

	// Specify report interval to send periodical reports for long TCP
	// connections. If not specified, the interval is 10 seconds. This interval
	// should not be less than 1 second, otherwise it will be reset to 1 second.
	reportInterval?: time.Duration @protobuf(6,type=google.protobuf.Duration,name=report_interval)
}
