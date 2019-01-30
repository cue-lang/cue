package kube

service <Name>: {
	apiVersion: "v1"
	kind:       "Service"
	metadata: {
		name: Name
		labels: {
			app:       Name       // by convention
			domain:    "prod"     // always the same in the given files
			component: _component // varies per directory
		}
	}
	spec: {
		// Any port has the following properties.
		ports: [...{
			port:     int
			protocol: *"TCP" | "UDP" // from the Kubernetes definition
			name:     *"client" | string
		}]
		selector: metadata.labels // we want those to be the same
	}
}

_component: string

daemonSet <Name>: _spec & {
	apiVersion: "extensions/v1beta1"
	kind:       "DaemonSet"
	_name:      Name
}

statefulSet <Name>: _spec & {
	apiVersion: "apps/v1beta1"
	kind:       "StatefulSet"
	_name:      Name
}

deployment <Name>: _spec & {
	apiVersion: "extensions/v1beta1"
	kind:       "Deployment"
	_name:      Name
	spec replicas: 1 | int
}

configMap <Name>: {
	metadata name: Name
	metadata labels component: _component
}

_spec: {
	_name: string
	metadata name: _name
	metadata labels component: _component
	spec template: {
		metadata labels: {
			app:       _name
			component: _component
			domain:    "prod"
		}
		spec containers: [{name: _name}]
	}
}

// Define the _export option and set the default to true
// for all ports defined in all containers.
_spec spec template spec containers: [...{
	ports: [...{
		_export: *true | false // include the port in the service
	}]
}]

service "\(k)": {
	spec selector: v.spec.template.metadata.labels

	spec ports: [ {
		Port = p.containerPort // Port is an alias
		port:       *Port | int
		targetPort: *Port | int
	} for c in v.spec.template.spec.containers
		for p in c.ports
		if p._export ]

} for x in [deployment, daemonSet, statefulSet] for k, v in x
