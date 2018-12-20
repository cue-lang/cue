package kube

service watcher: {
	kubernetes spec: {
		type:           "LoadBalancer"
		loadBalancerIP: "1.2.3.4" // static ip
	}
	ports https: {
		port:       7788
		targetPort: 7788
	}
}
