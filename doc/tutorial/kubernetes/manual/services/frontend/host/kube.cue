package kube

deployment: host: {
	replicas: 2
	image:    "gcr.io/myproj/host:v0.1.10"
}
