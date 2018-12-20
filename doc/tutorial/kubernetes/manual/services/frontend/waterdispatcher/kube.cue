package kube

deployment waterdispatcher: {
	image: "gcr.io/myproj/waterdispatcher:v0.0.48"
	arg http: ":8080"
	arg etcd: "etcd:2379"
}
