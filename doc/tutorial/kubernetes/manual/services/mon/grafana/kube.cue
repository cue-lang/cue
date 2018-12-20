package kube

deployment grafana: {
	image: "grafana/grafana:4.5.2"

	expose port grafana: 3000
	port web: 8080

	volume "grafana-volume": {
		mountPath: "/var/lib/grafana"
		spec gcePersistentDisk: {
			pdName: "grafana-volume"
			fsType: "ext4"
		}
	}

	// This variable is required to setup templates in Grafana.
	// The following env variables are required to make Grafana accessible via
	// the kubernetes api-server proxy. On production clusters, we recommend
	// removing these env variables, setup auth for grafana, and expose the grafana
	// service using a LoadBalancer or a public IP.
	env GF_AUTH_BASIC_ENABLED:      "false"
	env GF_AUTH_ANONYMOUS_ENABLED:  "true"
	env GF_AUTH_ANONYMOUS_ORG_ROLE: "admin"

	kubernetes spec template spec containers: [{
		// keep request = limit to keep this container in guaranteed class
		resources limits: {
			cpu:    "100m"
			memory: "100Mi"
		}
		resources requests: {
			cpu:    "100m"
			memory: "100Mi"
		}
	}]
}
