package kube

service: tasks: spec: {
	type:           "LoadBalancer"
	loadBalancerIP: "1.2.3.4" // static ip
	ports: [{
		port:       443
		targetPort: 7443
		name:       "http"
	}]
}
