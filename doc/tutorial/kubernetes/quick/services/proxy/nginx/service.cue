package kube

service: nginx: spec: {
	type:           "LoadBalancer"
	loadBalancerIP: "1.3.4.5"
	ports: [{
		port: 80 // the port that this service should serve on
		// the container on each pod to connect to, can be a name
		// (e.g. 'www') or a number (e.g. 80)
		targetPort: 80
		name:       "http"
	}, {
		port: 443
		name: "https"
	}]
}
