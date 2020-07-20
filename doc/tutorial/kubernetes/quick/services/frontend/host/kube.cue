package kube

service: host: spec: ports: [{
	port:       7080
	targetPort: 7080
}]
deployment: host: spec: {
	replicas: 2
	template: spec: containers: [{
		image: "gcr.io/myproj/host:v0.1.10"
		args: [
		]
	}]
}
