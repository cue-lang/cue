package kube

deployment authproxy: {
	image: "skippy/oauth2_proxy:2.0.1"
	args: ["--config=/etc/authproxy/authproxy.cfg"]

	expose port client: 4180

	volume "config-volume": {
		mountPath: "/etc/authproxy"
		spec configMap name: "authproxy"
	}
}
