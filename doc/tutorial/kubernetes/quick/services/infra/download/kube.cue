package kube

service: download: spec: ports: [{
	port:       7080
	targetPort: 7080
}]
deployment: download: spec: template: spec: containers: [{
	image: "gcr.io/myproj/download:v0.0.2"
	ports: [{
		containerPort: 7080
	}]
}]
