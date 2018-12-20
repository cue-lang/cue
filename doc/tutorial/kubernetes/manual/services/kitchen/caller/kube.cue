package kube

deployment caller: _kitchenDeployment & {
	replicas: 3
	image:    "gcr.io/myproj/caller:v0.20.14"

	arg key:  "/etc/certs/client.key"
	arg cert: "/etc/certs/client.pem"
	arg ca:   "/etc/certs/servfx.ca"

	arg "ssh-tunnel-key": "/sslcerts/tunnel-private.pem"

	volume "caller-disk": {
		name: "ssd-caller"
	}

	volume "secret-ssh-key": {
		mountPath: "/sslcerts"
		readOnly:  true
		spec secret secretName: "secrets"
	}
}
