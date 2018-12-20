package kube

deployment valeter: {
	image: "gcr.io/myproj/valeter:v0.0.4"
	arg http: ":8080"
	arg etcd: "etcd:2379"
	expose port http: 8080
}
