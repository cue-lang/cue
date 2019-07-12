package kube

service goget spec: {
	type:           "LoadBalancer"
	loadBalancerIP: "1.3.5.7" // static ip
	ports: [{
		port: 443
		name: "https"
	}]
}
