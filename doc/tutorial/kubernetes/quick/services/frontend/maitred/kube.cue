package kube

service: {}
deployment: maitred: spec: template: spec: containers: [{
	image: "gcr.io/myproj/maitred:v0.0.4"
	args: [
	]
}]
