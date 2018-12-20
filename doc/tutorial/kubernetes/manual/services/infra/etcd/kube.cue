package kube

service etcd kubernetes spec clusterIP: "None"

deployment etcd: {
	kind:     "stateful"
	replicas: 3

	image: "quay.io/coreos/etcd:v3.3.10"

	kubernetes spec template spec containers: [{command: ["/usr/local/bin/etcd"]}]

	arg name:                          "$(NAME)"
	arg "data-dir":                    "/data/etcd3"
	arg "initial-advertise-peer-urls": "http://$(IP):2380"
	arg "listen-peer-urls":            "http://$(IP):2380"
	arg "listen-client-urls":          "http://$(IP):2379,http://127.0.0.1:2379"
	arg "advertise-client-urls":       "http://$(IP):2379"
	arg discovery:                     "https://discovery.etcd.io/xxxxxx"

	env ETCDCTL_API:                    "3"
	env ETCD_AUTO_COMPACTION_RETENTION: "4"

	envSpec NAME valueFrom fieldRef fieldPath: "metadata.name"
	envSpec IP valueFrom fieldRef fieldPath:   "status.podIP"

	expose port client: 2379
	expose port peer:   2380

	kubernetes spec template spec containers: [{
		volumeMounts: [{
			name:      "etcd3"
			mountPath: "/data"
		}]
		livenessProbe: {
			httpGet: {
				path: "/health"
				port: "client"
			}
			initialDelaySeconds: 30
		}
	}]

	kubernetes spec: {
		volumeClaimTemplates: [{
			metadata: {
				name: "etcd3"
				annotations "volume.alpha.kubernetes.io/storage-class": "default"
			}
			spec: {
				accessModes: ["ReadWriteOnce"]
				resources requests storage: "10Gi"
			}
		}]

		serviceName: "etcd"
		template metadata annotations "prometheus.io.port":   "2379"
		template metadata annotations "prometheus.io.scrape": "true"
		template spec affinity: {
			podAntiAffinity requiredDuringSchedulingIgnoredDuringExecution: [{
				labelSelector matchExpressions: [{
					key:      "app"
					operator: "In"
					values: ["etcd"]
				}]
				topologyKey: "kubernetes.io/hostname"
			}]
		}
		template spec terminationGracePeriodSeconds: 10
	}
}
