package kube

service valeter spec ports: [{
	name: "http"
}]
deployment valeter spec template spec containers: [{
	image: "gcr.io/myproj/valeter:v0.0.4"
	ports: [{
		containerPort: 8080
	}]
	args: [
		"-http=:8080",
		"-etcd=etcd:2379",
	]
}]
