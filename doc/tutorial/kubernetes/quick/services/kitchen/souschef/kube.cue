package kube

service: souschef: spec: ports: [{
	port:       8080
	targetPort: 8080
}]
deployment: souschef: spec: template: spec: containers: [{
	image: "gcr.io/myproj/souschef:v0.5.3"
}]

deployment: souschef: spec: template: spec: _hasDisks: false
