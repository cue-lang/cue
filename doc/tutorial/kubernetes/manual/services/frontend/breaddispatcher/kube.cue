package kube

deployment breaddispatcher: {
	image: "gcr.io/myproj/breaddispatcher:v0.3.24"
	arg etcd:           "etcd:2379"
	arg "event-server": "events:7788"
}
