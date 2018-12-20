package kube

deployment goget spec: {
	// podTemplate defines the 'cookie cutter' used for creating
	// new pods when necessary
	template spec: {
		volumes: [{
			name: "secret-volume"
			secret secretName: "goget-secrets"
		}]
		containers: [{
			image: "gcr.io/myproj/goget:v0.5.1"
			ports: [{
				containerPort: 7443
			}]
			volumeMounts: [{
				mountPath: "/etc/ssl"
				name:      "secret-volume"
			}]
		}]
	}
}
