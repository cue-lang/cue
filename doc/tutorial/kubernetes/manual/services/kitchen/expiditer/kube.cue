package kube

deployment: expiditer: _kitchenDeployment & {
	image: "gcr.io/myproj/expiditer:v0.5.34"
	arg: "ssh-tunnel-key": "/etc/certs/tunnel-private.pem"
}
