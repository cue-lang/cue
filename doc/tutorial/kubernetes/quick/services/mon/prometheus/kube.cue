package kube

service: prometheus: {
	metadata: {
		annotations: "prometheus.io/scrape": "true"
		labels: name:                        "prometheus"
	}
	spec: {
		type: "NodePort"
		ports: [{
			name:     "main"
			nodePort: 30900
		}]
	}
}
deployment: prometheus: spec: {
	strategy: {
		rollingUpdate: {
			maxSurge:       0
			maxUnavailable: 1
		}
		type: "RollingUpdate"
	}
	selector: matchLabels: app: "prometheus"
	template: {
		metadata: {
			name: "prometheus"
			annotations: "prometheus.io.scrape": "true"
		}
		spec: {
			containers: [{
				image: "prom/prometheus:v2.4.3"
				args: [
					"--config.file=/etc/prometheus/prometheus.yml",
					"--web.external-url=https://prometheus.example.com",
				]
				ports: [{
					name:          "web"
					containerPort: 9090
				}]
				volumeMounts: [{
					name:      "config-volume"
					mountPath: "/etc/prometheus"
				}]
			}]
			volumes: [{
				name: "config-volume"
				configMap: name: "prometheus"
			}]
		}
	}
}
