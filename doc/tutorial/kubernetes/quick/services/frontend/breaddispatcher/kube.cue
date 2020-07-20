package kube

service: breaddispatcher: spec: ports: [{
	port:       7080
	targetPort: 7080
}]
deployment: breaddispatcher: spec: template: spec: containers: [{
	image: "gcr.io/myproj/breaddispatcher:v0.3.24"
	args: [
		"-etcd=etcd:2379",
		"-event-server=events:7788",
	]
}]
