package kube

deployment souschef spec template spec containers: [{
	image: "gcr.io/myproj/souschef:v0.5.3"
}]

deployment souschef spec template spec: {
	hasDisks :: false
}
