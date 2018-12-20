package kube

_component: "frontend"

deployment <X> spec template: {
	metadata annotations: {
		"prometheus.io.scrape": "true"
		"prometheus.io.port":   "\(spec.containers[0].ports[0].containerPort)"
	}
	spec containers: [{
		ports: [{containerPort: 7080 | int}] // 7080 is the default
	}]
}
