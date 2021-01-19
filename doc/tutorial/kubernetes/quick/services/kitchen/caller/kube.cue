package kube

deployment: caller: spec: {
	replicas: 3
	template: spec: {
		volumes: [{
			name: "ssd-caller"
			gcePersistentDisk: {
				// This disk must already exist.
				pdName: "ssd-caller"
			}
		}, {
		}, {
			name: "secret-ssh-key"
			secret: secretName: "secrets"
		}]
		containers: [{
			image: "gcr.io/myproj/caller:v0.20.14"
			volumeMounts: [{
				name: "ssd-caller"
			}, {
			}, {
				mountPath: "/sslcerts"
				name:      "secret-ssh-key"
				readOnly:  true
			}]
			args: [
				"-env=prod",
				"-key=/etc/certs/client.key",
				"-cert=/etc/certs/client.pem",
				"-ca=/etc/certs/servfx.ca",
				"-ssh-tunnel-key=/sslcerts/tunnel-private.pem",
				"-logdir=/logs",
				"-event-server=events:7788",
			]
		}]
	}
}
