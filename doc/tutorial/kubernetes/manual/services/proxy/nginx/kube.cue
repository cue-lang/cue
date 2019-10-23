package kube

deployment: nginx: {
	image: "nginx:1.11.10-alpine"

	expose: port: http:  80
	expose: port: https: 443

	volume: "secret-volume": {
		mountPath: "/etc/ssl"
		spec: secret: secretName: "proxy-secrets"
	}

	volume: "config-volume": {
		mountPath: "/etc/nginx/nginx.conf"
		subPath:   "nginx.conf"
		spec: configMap: name: "nginx"
	}
}
