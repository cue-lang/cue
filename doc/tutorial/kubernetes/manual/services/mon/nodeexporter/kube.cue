package kube

service "node-exporter": {
	port scrape name: "metrics"

	kubernetes metadata annotations "prometheus.io/scrape": "true"
	kubernetes spec type:      "ClusterIP"
	kubernetes spec clusterIP: "None"
}

deployment "node-exporter": {
	kind: "daemon"

	image: "quay.io/prometheus/node-exporter:v0.16.0"

	expose port scrape: 9100
	args: ["--path.procfs=/host/proc", "--path.sysfs=/host/sys"]

	volume proc: {
		mountPath: "/host/proc"
		readOnly:  true
		spec hostPath path: "/proc"
	}
	volume sys: {
		mountPath: "/host/sys"
		readOnly:  true
		spec hostPath path: "/sys"
	}

	kubernetes spec template spec: {
		hostNetwork: true
		hostPID:     true

		containers: [{
			ports: [{hostPort: 9100}]
			resources requests: {
				memory: "30Mi"
				cpu:    "100m"
			}
			resources limits: {
				memory: "50Mi"
				cpu:    "200m"
			}
		}]
	}
}
