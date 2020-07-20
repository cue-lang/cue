package kube

service: bartender: spec: ports: [{
	port:       7080
	targetPort: 7080
}]
deployment: bartender: spec: template: spec: containers: [{
	image: "gcr.io/myproj/bartender:v0.1.34"
	args: [
	]
}]
