package kube

service watcher spec: {
	type:           "LoadBalancer"
	loadBalancerIP: "1.2.3.4."
	// static ip
	ports: [{
		name: "http"
	}]
}
