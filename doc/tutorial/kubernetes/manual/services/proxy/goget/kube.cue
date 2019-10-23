package kube

deployment: goget: {
	image: "gcr.io/myproj/goget:v0.5.1"

	expose: port: https: 7443

	volume: "secret-volume": {
		mountPath: "/etc/ssl"
		spec: secret: secretName: "goget-secrets"
	}
}
