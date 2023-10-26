package kube

kubernetes: services: {
	for k, x in service {
		"\(k)": x.kubernetes & {
			apiVersion: "v1"
			kind:       "Service"

			metadata: name:   x.name
			metadata: labels: x.label
			spec: selector:   x.label

			spec: ports: [for p in x.port {p}] // convert struct to list
		}
	}
	// Note that we cannot write
	//   kubernetes services "\(k)": {} for k, x in service
	// "service" is also a field comprehension and the spec prohibits one field
	// comprehension referencing another within the same struct.
	// In general it is good practice to define a comprehension in the smallest
	// struct possible.
}

// TODO: with type conversions and types, if implemented:
// deployments :: k8s.Deployment
// deployments: _k8sSpec(X: x) for x in deployment
// This would look nicer and would allow for superior type checking.

kubernetes: deployments: {
	for k, x in deployment if x.kind == "deployment" {
		"\(k)": (_k8sSpec & {X: x}).X.kubernetes & {
			apiVersion: "extensions/v1beta1"
			kind:       "Deployment"
			spec: replicas: x.replicas
		}
	}
}

kubernetes: statefulSets: {
	for k, x in deployment if x.kind == "stateful" {
		"\(k)": (_k8sSpec & {X: x}).X.kubernetes & {
			apiVersion: "apps/v1beta1"
			kind:       "StatefulSet"
			spec: replicas: x.replicas
		}
	}
}

kubernetes: daemonSets: {
	for k, x in deployment if x.kind == "daemon" {
		"\(k)": (_k8sSpec & {X: x}).X.kubernetes & {
			apiVersion: "extensions/v1beta1"
			kind:       "DaemonSet"
		}
	}
}

kubernetes: configMaps: {
	for k, v in configMap {
		"\(k)": {
			apiVersion: "v1"
			kind:       "ConfigMap"

			metadata: name: k
			metadata: labels: component: _base.label.component
			data: v
		}
	}
}

// _k8sSpec injects Kubernetes definitions into a deployment
// Unify the deployment at X and read out kubernetes to obtain
// the conversion.
// TODO: use alias
_k8sSpec: X: kubernetes: {
	metadata: name: X.name
	metadata: labels: component: X.label.component

	spec: template: {
		metadata: labels: X.label

		spec: containers: [{
			name:  X.name
			image: X.image
			args:  X.args
			if len(X.envSpec) > 0 {
				env: [for k, v in X.envSpec {v, name: k}]
			}

			ports: [for k, p in X.expose.port & X.port {
				name:          k
				containerPort: p
			}]
		}]
	}

	// Volumes
	spec: template: spec: {
		if len(X.volume) > 0 {
			volumes: [
				for v in X.volume {
					v.kubernetes

					name: v.name
				},
			]
		}

		containers: [{
			// TODO: using conversions this would look like:
			// volumeMounts: [ k8s.VolumeMount(v) for v in d.volume ]
			if len(X.volume) > 0 {
				volumeMounts: [
					for v in X.volume {
						name:      v.name
						mountPath: v.mountPath
						if v.subPath != null {
							subPath: v.subPath
						}
						if v.readOnly {
							readOnly: v.readOnly
						}
					},
				]
			}
		}]
	}
}
