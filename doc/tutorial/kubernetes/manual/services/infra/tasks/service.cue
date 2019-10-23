package kube

service: tasks: {
	port: https: {
		port:       443
		targetPort: 7443
		protocol:   "TCP"
	}
	kubernetes: spec: {
		type:           "LoadBalancer"
		loadBalancerIP: "1.2.3.4"
	}
}
