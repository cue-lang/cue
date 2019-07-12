package kube

service waterdispatcher spec ports: [{
	name: "http"
}]
deployment waterdispatcher spec template spec containers: [{
	image: "gcr.io/myproj/waterdispatcher:v0.0.48"
	args: [
		"-http=:8080",
		"-etcd=etcd:2379",
	]
}]
