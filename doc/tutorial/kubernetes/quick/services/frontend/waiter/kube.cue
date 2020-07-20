package kube

service: waiter: spec: ports: [{
	port:       7080
	targetPort: 7080
}]
deployment: waiter: spec: {
	replicas: 5
	template: spec: containers: [{
		image: "gcr.io/myproj/waiter:v0.3.0"
	}]
}
