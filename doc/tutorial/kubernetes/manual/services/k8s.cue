package kube

kubernetes services: {
	"\(k)": x.kubernetes & {
		apiVersion: "v1"
		kind:       "Service"

		metadata name:   x.name
		metadata labels: x.label
		spec selector:   x.label

		spec ports: [ p for p in x.port ]
// jba: how does [p for p in x.port ] differ from x.port?
	} for k, x in service
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

kubernetes deployments: {
	"\(k)": (_k8sSpec & {X: x}).X.kubernetes & {
		apiVersion: "extensions/v1beta1"
		kind:       "Deployment"
		spec replicas: x.replicas
	} for k, x in deployment if x.kind == "deployment"
}

kubernetes statefulSets: {
	"\(k)": (_k8sSpec & {X: x}).X.kubernetes & {
		apiVersion: "apps/v1beta1"
		kind:       "StatefulSet"
		spec replicas: x.replicas
	} for k, x in deployment if x.kind == "stateful"
}

kubernetes daemonSets: {
	"\(k)": (_k8sSpec & {X: x}).X.kubernetes & {
		apiVersion: "extensions/v1beta1"
		kind:       "DaemonSet"
	} for k, x in deployment if x.kind == "daemon"
}

kubernetes configMaps: {
	"\(k)": {
		apiVersion: "v1"
		kind:       "ConfigMap"

		metadata name: k
		metadata labels component: _base.label.component
		data: v
	} for k, v in configMap
}

// _k8sSpec injects Kubernetes definitions into a deployment
// Unify the deployment at X and read out kubernetes to obtain
// the conversion.
// TODO: use alias
_k8sSpec X kubernetes: {
	metadata name: X.name
	metadata labels component: X.label.component

	spec template: {
		metadata labels: X.label

		spec containers: [{
			name:  X.name
			image: X.image
			args:  X.args
			env:   [ {name: k} & v for k, v in X.envSpec ] if len(X.envSpec) > 0

			ports: [ {
				name:          k
				containerPort: p
			} for k, p in X.expose.port & X.port ]
		}]
	}

	// Volumes
	spec template spec: {
		volumes: [
				v.kubernetes & {name: v.name} for v in X.volume
		] if len(X.volume) > 0

		containers: [{
			// TODO: using conversions this would look like:
			// volumeMounts: [ k8s.VolumeMount(v) for v in d.volume ]
			volumeMounts: [ {
				name:      v.name
				mountPath: v.mountPath
				subPath:   v.subPath if v.subPath != null | true
				readOnly:  v.readOnly if v.readOnly
			} for v in X.volume
			] if len(X.volume) > 0
		}]
	}
}
