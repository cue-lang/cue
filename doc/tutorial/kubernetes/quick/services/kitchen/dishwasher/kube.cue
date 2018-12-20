package kube

deployment dishwasher spec: {
	replicas: 5
	template spec: {
		volumes: [{
		}, {
		}, {
			name: "secret-ssh-key"
			secret secretName: "dishwasher-secrets"
		}]
		containers: [{
			image: "gcr.io/myproj/dishwasher:v0.2.13"
			volumeMounts: [{
			}, {
				mountPath: "/sslcerts"
			}, {
				mountPath: "/etc/certs"
				name:      "secret-ssh-key"
				readOnly:  true
			}]
			args: ["-env=prod", "-ssh-tunnel-key=/etc/certs/tunnel-private.pem", "-logdir=/logs", "-event-server=events:7788"]
		}]
	}
}
