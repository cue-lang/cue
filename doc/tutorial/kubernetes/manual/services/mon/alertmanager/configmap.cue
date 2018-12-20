package kube

import "encoding/yaml"

configMap alertmanager: {
	"alerts.yaml": yaml.Marshal(alerts_yaml)
	alerts_yaml = {
		receivers: [{
			name: "pager"
			// email_configs:
			// - to: 'team-X+alerts-critical@example.org'
			slack_configs: [{
				channel: "#cloudmon"
				text: """
		{{ range .Alerts }}{{ .Annotations.description }}
		{{ end }}
		"""
				send_resolved: true
			}]
		}]
		// The root route on which each incoming alert enters.
		route: {
			receiver: "pager"
			// The labels by which incoming alerts are grouped together. For example,
			// multiple alerts coming in for cluster=A and alertname=LatencyHigh would
			// be batched into a single group.
			group_by: ["alertname", "cluster"]
		}
	}
}
