package kube

service: alertmanager: {
	label: name: "alertmanager"

	port: alertmanager: name: "main"

	kubernetes: metadata: {
		annotations: "prometheus.io/scrape": "true"
		annotations: "prometheus.io/path":   "/metrics"
	}
}

deployment: alertmanager: {
	kubernetes: spec: selector: matchLabels: app: "alertmanager"

	image: "prom/alertmanager:v0.15.2"

	args: [
		"--config.file=/etc/alertmanager/alerts.yaml",
		"--storage.path=/alertmanager",
		"--web.external-url=https://alertmanager.example.com",
	]

	// XXX: adding another label cause an error at the wrong position:
	// expose port alertmanager configMap
	expose: port: alertmanager: 9093

	volume: "config-volume": {
		mountPath: "/etc/alertmanager"
		spec: configMap: name: "alertmanager"
	}
	volume: alertmanager: {
		mountPath: "/alertmanager"
		spec: emptyDir: {}
	}
}
