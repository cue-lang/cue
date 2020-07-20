package kube

service: headchef: spec: ports: [{
	port:       8080
	targetPort: 8080
}]
deployment: headchef: spec: template: spec: containers: [{
	image: "gcr.io/myproj/headchef:v0.2.16"
	volumeMounts: [{
	}, {
		mountPath: "/sslcerts"
	}]
	args: [
		"-env=prod",
		"-logdir=/logs",
		"-event-server=events:7788",
	]
}]
