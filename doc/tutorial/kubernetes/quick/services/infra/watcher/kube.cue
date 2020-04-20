package kube

deployment: watcher: spec: {
	// podTemplate defines the 'cookie cutter' used for creating
	// new pods when necessary
	template: {
		spec: {
			volumes: [{
				name: "secret-volume"
				secret: secretName: "star-example-com-secrets"
			}]
			containers: [{
				image: "gcr.io/myproj/watcher:v0.1.0"
				ports: [{
					containerPort: 7080
				}, {
					containerPort: 7788
				}]
				volumeMounts: [{
					mountPath: "/etc/ssl"
					name:      "secret-volume"
				}]
			}]
		}
	}
}

deployment: watcher: spec: template: spec: containers: [{ports: [{_export: false}, _]}]
