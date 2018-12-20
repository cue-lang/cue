package kube

service etcd spec: {
	clusterIP: "None"
	ports: [{
	}, {
		name: "peer"
	}]
}
statefulSet etcd spec: {
	serviceName: "etcd"
	replicas:    3
	template: {
		metadata annotations: {
			"prometheus.io.scrape": "true"
			"prometheus.io.port":   "2379"
		}
		spec: {
			affinity podAntiAffinity requiredDuringSchedulingIgnoredDuringExecution: [{
				labelSelector matchExpressions: [{
					key:      "app"
					operator: "In"
					values: ["etcd"]
				}]
				topologyKey: "kubernetes.io/hostname"
			}]
			terminationGracePeriodSeconds: 10
			containers: [{
				image: "quay.io/coreos/etcd:v3.3.10"
				ports: [{
					name:          "client"
					containerPort: 2379
				}, {
					name:          "peer"
					containerPort: 2380
				}]
				livenessProbe: {
					httpGet: {
						path: "/health"
						port: "client"
					}
					initialDelaySeconds: 30
				}
				volumeMounts: [{
					name:      "etcd3"
					mountPath: "/data"
				}]
				env: [{
					name:  "ETCDCTL_API"
					value: "3"
				}, {
					name:  "ETCD_AUTO_COMPACTION_RETENTION"
					value: "4"
				}, {
					name: "NAME"
					valueFrom fieldRef fieldPath: "metadata.name"
				}, {
					name: "IP"
					valueFrom fieldRef fieldPath: "status.podIP"
				}]
				command: ["/usr/local/bin/etcd"]
				args: ["-name", "$(NAME)", "-data-dir", "/data/etcd3", "-initial-advertise-peer-urls", "http://$(IP):2380", "-listen-peer-urls", "http://$(IP):2380", "-listen-client-urls", "http://$(IP):2379,http://127.0.0.1:2379", "-advertise-client-urls", "http://$(IP):2379", "-discovery", "https://discovery.etcd.io/xxxxxx"]
			}]
		}
	}
	// bootstrap
	// "-initial-cluster-token", "etcd-prod-events2",
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
}
