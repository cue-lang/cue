package kube

deployment: expiditer: spec: template: spec: containers: [{
	image: "gcr.io/myproj/expiditer:v0.5.34"
	args: [
		"-env=prod",
		"-ssh-tunnel-key=/etc/certs/tunnel-private.pem",
		"-logdir=/logs",
		"-event-server=events:7788",
	]
}]
