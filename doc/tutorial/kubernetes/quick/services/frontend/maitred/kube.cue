package kube

service: maitred: spec: ports: [{
	port:       7080
	targetPort: 7080
}]
deployment: maitred: spec: template: spec: containers: [{
	image: "gcr.io/myproj/maitred:v0.0.4"
	args: [
	]
}]
