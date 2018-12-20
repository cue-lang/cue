package kube

service prometheus: {
	label name: "prometheus"
	port web: {
		name:     "main"
		nodePort: 30900
	}
	kubernetes metadata annotations "prometheus.io/scrape": "true"
	kubernetes spec type: "NodePort"
}

deployment prometheus: {
	image: "prom/prometheus:v2.4.3"
	args: [
		"--config.file=/etc/prometheus/prometheus.yml",
		"--web.external-url=https://prometheus.example.com",
	]

	expose port web: 9090

	volume "config-volume": {
		mountPath: "/etc/prometheus"
		spec configMap name: "prometheus"
	}

	kubernetes spec selector matchLabels app: "prometheus"

	kubernetes spec strategy: {
		type: "RollingUpdate"
		rollingUpdate: {
			maxSurge:       0
			maxUnavailable: 1
		}
	}
	kubernetes spec template metadata annotations "prometheus.io.scrape": "true"
}
