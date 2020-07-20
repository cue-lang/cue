package kube

service: watcher: spec: {
	type:           "LoadBalancer"
	loadBalancerIP: "1.2.3.4." // static ip
	ports: [{
		port:       7788
		targetPort: 7788
		name:       "http"
	}]
}
