package kube

deployment pastrychef: _kitchenDeployment & {
	image: "gcr.io/myproj/pastrychef:v0.1.15"

	volume "secret-pastrychef": {
		name: "secret-ssh-key"
		spec secret secretName: "secrets"
	}

	arg "ssh-tunnel-key":   "/etc/certs/tunnel-private.pem"
	arg "reconnect-delay":  "1m"
	arg etcd:               "etcd:2379"
	arg "recovery-overlap": "10000"
}
