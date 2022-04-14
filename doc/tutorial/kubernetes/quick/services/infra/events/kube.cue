package kube

service: events: spec: ports: [{
	name: "grpc"
}]
deployment: events: spec: {
	replicas: 2
	template: {
		metadata: annotations: {
			"prometheus.io.scrape": "true"
			"prometheus.io.port":   "7080"
		}
		spec: {
			affinity: podAntiAffinity: requiredDuringSchedulingIgnoredDuringExecution: [{
				labelSelector: matchExpressions: [{
					key:      "app"
					operator: "In"
					values: [
						"events",
					]
				}]
				topologyKey: "kubernetes.io/hostname"
			}]
			volumes: [{
				name: "secret-volume"
				secret: secretName: "biz-secrets"
			}]
			containers: [{
				image: "gcr.io/myproj/events:v0.1.31"
				ports: [{
					containerPort: 7080
				}, {
					containerPort: 7788
				}]
				args: [
					"-cert=/etc/ssl/server.pem",
					"-key=/etc/ssl/server.key",
					"-grpc=:7788",
				]
				volumeMounts: [{
					mountPath: "/etc/ssl"
					name:      "secret-volume"
				}]
			}]
		}
	}
}

deployment: events: spec: template: spec: containers: [{ports: [{_export: false}, _]}]
