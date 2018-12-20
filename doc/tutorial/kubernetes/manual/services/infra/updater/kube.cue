package kube

deployment updater: {
	image: "gcr.io/myproj/updater:v0.1.0"
	args: ["-key=/etc/certs/updater.pem"]

	expose port http: 8080
	volume "secret-updater": {
		mountPath: "/etc/certs"
		spec secret secretName: "updater-secrets"
	}
}
