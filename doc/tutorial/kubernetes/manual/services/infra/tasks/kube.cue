package kube

deployment tasks: {
	image: "gcr.io/myproj/tasks:v0.2.6"

	port http: 7080
	expose port https: 7443

	volume "secret-volume": {
		mountPath: "/etc/ssl"
		spec secret secretName: "star-example-com-secrets"
	}

	kubernetes spec template metadata annotations: {
		"prometheus.io.port":   "7080"
		"prometheus.io.scrape": "true"
	}
}
