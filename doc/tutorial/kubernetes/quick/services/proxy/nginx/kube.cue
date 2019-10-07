package kube

deployment: nginx: spec: {
	// podTemplate defines the 'cookie cutter' used for creating
	// new pods when necessary
	template: spec: {
		volumes: [{
			name: "secret-volume"
			secret: secretName: "proxy-secrets"
		}, {
			name: "config-volume"
			configMap: name: "nginx"
		}]
		containers: [{
			image: "nginx:1.11.10-alpine"
			ports: [{
				containerPort: 80
			}, {
				containerPort: 443
			}]
			volumeMounts: [{
				mountPath: "/etc/ssl"
				name:      "secret-volume"
			}, {
				name:      "config-volume"
				mountPath: "/etc/nginx/nginx.conf"
				subPath:   "nginx.conf"
			}]
		}]
	}
}
