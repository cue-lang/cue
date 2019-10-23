package kube

deployment: events: {
	replicas: 2
	image:    "gcr.io/myproj/events:v0.1.31"

	arg: cert: "/etc/ssl/server.pem"
	arg: key:  "/etc/ssl/server.key"
	arg: grpc: ":7788"

	port: http: 7080
	expose: port: grpc: 7788

	volume: "secret-volume": {
		mountPath: "/etc/ssl"
		spec: secret: secretName: "biz-secrets"
	}

	kubernetes: spec: template: metadata: annotations: {
		"prometheus.io.port":   "7080"
		"prometheus.io.scrape": "true"
	}

	kubernetes: spec: template: spec: affinity: podAntiAffinity: requiredDuringSchedulingIgnoredDuringExecution: [{
		labelSelector: matchExpressions: [{
			key:      "app"
			operator: "In"
			values: ["events"]
		}]
		topologyKey: "kubernetes.io/hostname"
	}]
}
