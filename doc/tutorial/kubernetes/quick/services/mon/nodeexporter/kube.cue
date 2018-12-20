package kube

service "node-exporter": {
	metadata annotations "prometheus.io/scrape": "true"
	spec: {
		type:      "ClusterIP"
		clusterIP: "None"
		ports: [{
			name: "metrics"
		}]
	}
}
daemonSet "node-exporter" spec template: {
	metadata name: "node-exporter"
	spec: {
		hostNetwork: true
		hostPID:     true
		containers: [{
			image: "quay.io/prometheus/node-exporter:v0.16.0"
			args: ["--path.procfs=/host/proc", "--path.sysfs=/host/sys"]
			ports: [{
				containerPort: 9100
				hostPort:      9100
				name:          "scrape"
			}]
			resources: {
				requests: {
					memory: "30Mi"
					cpu:    "100m"
				}
				limits: {
					memory: "50Mi"
					cpu:    "200m"
				}
			}
			volumeMounts: [{
				name:      "proc"
				readOnly:  true
				mountPath: "/host/proc"
			}, {
				name:      "sys"
				readOnly:  true
				mountPath: "/host/sys"
			}]
		}]
		volumes: [{
			name: "proc"
			hostPath path: "/proc"
		}, {
			name: "sys"
			hostPath path: "/sys"
		}]
	}
}
