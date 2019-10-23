package kube

_base: label: component: "frontend"

deployment: [string]: {
	expose: port: http: *7080 | int
	kubernetes: spec: template: metadata: annotations: {
		"prometheus.io.scrape": "true"
		"prometheus.io.port":   "\(expose.port.http)"
	}
}
