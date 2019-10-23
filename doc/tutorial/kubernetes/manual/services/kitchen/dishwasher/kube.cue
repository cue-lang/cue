package kube

deployment: dishwasher: _kitchenDeployment & {
	replicas: 5
	image:    "gcr.io/myproj/dishwasher:v0.2.13"
	arg: "ssh-tunnel-key": "/etc/certs/tunnel-private.pem"
	volume: "secret-ssh-key": {
		mountPath: "/sslcerts"
		readOnly:  true
		spec: secret: secretName: "secrets"
	}
}
