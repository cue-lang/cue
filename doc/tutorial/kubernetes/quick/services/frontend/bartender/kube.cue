package kube

deployment: bartender: spec: template: spec: containers: [{
	image: "gcr.io/myproj/bartender:v0.1.34"
	args: [
	]
}]
