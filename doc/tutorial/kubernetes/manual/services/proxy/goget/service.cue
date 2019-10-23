package kube

service: goget: {
	port: http: {port: 443}

	kubernetes: spec: {
		type:           "LoadBalancer"
		loadBalancerIP: "1.3.5.7" // static ip
	}
}
