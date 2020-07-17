package kube

service: nginx: spec: {
	type:           "LoadBalancer"
	loadBalancerIP: "1.3.4.5"
	ports: [{
		name: "http"
	}, {
		name: "https"
	}]
}
