package kube

deployment waiter: {
	image:    "gcr.io/myproj/waiter:v0.3.0"
	replicas: 5
}
