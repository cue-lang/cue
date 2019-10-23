package kube

deployment: download: {
	image: "gcr.io/myproj/download:v0.0.2"
	expose: port: client: 7080
}
