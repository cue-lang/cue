package kube

_base label component: "kitchen"

deployment <Name>: {
	expose port client: 8080

	kubernetes spec template metadata annotations "prometheus.io.scrape": "true"

	kubernetes spec template spec containers: [{
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

// _kitchenDeployment provides a basis configuration for kitchen deployments.
_kitchenDeployment: {
	name: string

	arg env:            "prod"
	arg logdir:         "/logs"
	arg "event-server": "events:7788"

	// Volumes
	volume "\(name)-disk": {
		name:      string
		mountPath: *"/logs" | string
		spec gcePersistentDisk: {
			pdName: *name | string
			fsType: "ext4"
		}
	}

	volume "secret-\(name)": {
		mountPath: *"/etc/certs" | string
		readOnly:  true
		spec secret secretName: *"\(name)-secrets" | string
	}
}
