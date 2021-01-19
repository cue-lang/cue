package kube

deployment: pastrychef: spec: template: spec: {
	volumes: [{
	}, {
		name: "secret-ssh-key"
		secret: secretName: "secrets"
	}]
	containers: [{
		image: "gcr.io/myproj/pastrychef:v0.1.15"
		volumeMounts: [{
		}, {
			name: "secret-ssh-key"
		}]
		args: [
			"-env=prod",
			"-ssh-tunnel-key=/etc/certs/tunnel-private.pem",
			"-logdir=/logs",
			"-event-server=events:7788",
			"-reconnect-delay=1m",
			"-etcd=etcd:2379",
			"-recovery-overlap=10000",
		]
	}]
}
