package kube

deployment: waiter: spec: {
	replicas: 5
	template: spec: containers: [{
		image: "gcr.io/myproj/waiter:v0.3.0"
	}]
}
