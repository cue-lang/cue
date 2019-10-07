package kube

deployment: grafana: {
	metadata: labels: app: "grafana"
	spec: template: spec: {
		volumes: [{
			name: "grafana-volume"
			gcePersistentDisk: {
				// This disk must already exist.
				pdName: "grafana-volume"
				fsType: "ext4"
			}
		}]
		containers: [{
			image: "grafana/grafana:4.5.2"
			ports: [{
				containerPort: 8080
			}]
			resources: {
				// keep request = limit to keep this container in guaranteed class
				limits: {
					cpu:    "100m"
					memory: "100Mi"
				}
				requests: {
					cpu:    "100m"
					memory: "100Mi"
				}
			}
			env: [{
				// This variable is required to setup templates in Grafana.
				// The following env variables are required to make Grafana accessible via
				// the kubernetes api-server proxy. On production clusters, we recommend
				// removing these env variables, setup auth for grafana, and expose the grafana
				// service using a LoadBalancer or a public IP.
				name:  "GF_AUTH_BASIC_ENABLED"
				value: "false"
			}, {
				name:  "GF_AUTH_ANONYMOUS_ENABLED"
				value: "true"
			}, {
				name:  "GF_AUTH_ANONYMOUS_ORG_ROLE"
				value: "admin"
			}]
			volumeMounts: [{
				name:      "grafana-volume"
				mountPath: "/var/lib/grafana"
			}]
		}]
	}
}
service: grafana: spec: ports: [{
	name:       "grafana"
	port:       3000
	targetPort: 3000
}]
