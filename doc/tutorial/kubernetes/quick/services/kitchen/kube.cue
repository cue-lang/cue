package kube

_component: "kitchen"

deployment <Name> spec template: {
	metadata annotations "prometheus.io.scrape": "true"
	spec containers: [{
		ports: [{
			containerPort: 8080
		}]
		livenessProbe: {
			httpGet: {
				path: "/debug/health"
				port: 8080
			}
			initialDelaySeconds: 40
			periodSeconds:       3
		}
	}]
}

deployment <Name> spec template spec: {
	_hasDisks: true | bool

	volumes: [{
		name: "\(Name)-disk" | string
		gcePersistentDisk pdName: "\(Name)-disk" | string
		gcePersistentDisk fsType: "ext4"
	}, {
		name: "secret-\(Name)" | string
		secret secretName: "\(Name)-secrets" | string
	}, ...] if _hasDisks

	containers: [{
		volumeMounts: [{
			name:      "\(Name)-disk" | string
			mountPath: "/logs" | string
		}, {
			mountPath: "/etc/certs" | string
			name:      "secret-\(Name)" | string
			readOnly:  true
		}, ...]
	}] if _hasDisks // field comprehension using just "if"
}
