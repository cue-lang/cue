package kube

deployment host spec: {
	replicas: 2
	template spec containers: [{
		image: "gcr.io/myproj/host:v0.1.10"
		args: []
	}]
}
