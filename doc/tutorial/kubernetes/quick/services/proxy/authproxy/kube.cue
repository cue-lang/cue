package kube

deployment authproxy spec: {
	// podTemplate defines the 'cookie cutter' used for creating
	// new pods when necessary
	template spec: {
		containers: [{
			image: "skippy/oauth2_proxy:2.0.1"
			ports: [{
				containerPort: 4180
			}]
			args: [
				"--config=/etc/authproxy/authproxy.cfg",
			]

			volumeMounts: [{
				name:      "config-volume"
				mountPath: "/etc/authproxy"
			}]
		}]
		volumes: [{
			name: "config-volume"
			configMap name: "authproxy"
		}]
	}
}
