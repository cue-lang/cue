package kube

deployment headchef: _kitchenDeployment & {
	image: "gcr.io/myproj/headchef:v0.2.16"
	volume "secret-headchef" mountPath: "/sslcerts"
}
