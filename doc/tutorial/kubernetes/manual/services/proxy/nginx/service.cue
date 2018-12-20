package kube

service nginx kubernetes spec: {
	type:           "LoadBalancer"
	loadBalancerIP: "1.3.4.5"
}
