package kube

deployment: watcher: {
	image: "gcr.io/myproj/watcher:v0.1.0"

	volume: "secret-volume": {
		mountPath: "/etc/ssl"
		spec: secret: secretName: "star-example-com-secrets"
	}
	port: http: 7080
	expose: port: https: 7788
}
