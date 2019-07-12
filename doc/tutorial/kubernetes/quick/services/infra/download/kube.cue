package kube

deployment download spec template spec containers: [{
	image: "gcr.io/myproj/download:v0.0.2"
	ports: [{containerPort: 7080}]
}]
