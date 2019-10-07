package kube

service: alertmanager: {
	metadata: {
		annotations: {
			"prometheus.io/scrape": "true"
			"prometheus.io/path":   "/metrics"
		}
		labels: name: "alertmanager"
	}
	spec: {
		// type: ClusterIP
		ports: [{
			name: "main"
		}]
	}
}
deployment: alertmanager: spec: {
	selector: matchLabels: app: "alertmanager"
	template: {
		metadata: name: "alertmanager"
		spec: {
			containers: [{
				image: "prom/alertmanager:v0.15.2"
				args: [
					"--config.file=/etc/alertmanager/alerts.yaml",
					"--storage.path=/alertmanager",
					"--web.external-url=https://alertmanager.example.com",
				]
				ports: [{
					name:          "alertmanager"
					containerPort: 9093
				}]
				volumeMounts: [{
					name:      "config-volume"
					mountPath: "/etc/alertmanager"
				}, {
					name:      "alertmanager"
					mountPath: "/alertmanager"
				}]
			}]
			volumes: [{
				name: "config-volume"
				configMap: name: "alertmanager"
			}, {
				name: "alertmanager"
				emptyDir: {}
			}]
		}
	}
}
