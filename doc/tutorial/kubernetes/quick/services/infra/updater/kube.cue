package kube

service: updater: spec: ports: [{
	port:       8080
	targetPort: 8080
}]
deployment: updater: spec: template: spec: {
	volumes: [{
		name: "secret-updater"
		secret: secretName: "updater-secrets"
	}]
	containers: [{
		image: "gcr.io/myproj/updater:v0.1.0"
		volumeMounts: [{
			mountPath: "/etc/certs"
			name:      "secret-updater"
		}]

		ports: [{
			containerPort: 8080
		}]
		args: [
			"-key=/etc/certs/updater.pem",
		]
	}]
}
