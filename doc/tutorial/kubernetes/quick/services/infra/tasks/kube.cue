package kube

deployment: tasks: spec: {
	// podTemplate defines the 'cookie cutter' used for creating
	// new pods when necessary
	template: {
		metadata: annotations: {
			"prometheus.io.scrape": "true"
			"prometheus.io.port":   "7080"
		}
		spec: {
			volumes: [{
				name: "secret-volume"
				secret: secretName: "star-example-com-secrets"
			}]
			containers: [{
				image: "gcr.io/myproj/tasks:v0.2.6"
				ports: [{
					containerPort: 7080
				}, {
					containerPort: 7443
				}]
				volumeMounts: [{
					mountPath: "/etc/ssl"
					name:      "secret-volume"
				}]
			}]
		}
	}
}

deployment: tasks: spec: template: spec: containers: [{ports: [{_export: false}, _]}]
