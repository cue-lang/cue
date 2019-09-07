package kube

service <ID>: {
	apiVersion: "v1"
	kind:       "Service"
	metadata: {
		name: ID
		labels: {
			app:       ID        // by convention
			domain:    "prod"    // always the same in the given files
			component: Component // varies per directory
		}
	}
	spec: {
		// Any port has the following properties.
		ports: [...{
			port:     int
			protocol: *"TCP" | "UDP" // from the Kubernetes definition
			name:     string | *"client"
		}]
		selector: metadata.labels // we want those to be the same
	}
}

deployment <ID>: {
	apiVersion: "extensions/v1beta1"
	kind:       "Deployment"
	metadata name: ID
	spec: {
		// 1 is the default, but we allow any number
		replicas: *1 | int
		template: {
			metadata labels: {
				app:       ID
				domain:    "prod"
				component: Component
			}
			// we always have one namesake container
			spec containers: [{name: ID}]
		}
	}
}

Component :: string

daemonSet <ID>: _spec & {
	apiVersion: "extensions/v1beta1"
	kind:       "DaemonSet"
	Name ::     ID
}

statefulSet <ID>: _spec & {
	apiVersion: "apps/v1beta1"
	kind:       "StatefulSet"
	Name ::     ID
}

deployment <ID>: _spec & {
	apiVersion: "extensions/v1beta1"
	kind:       "Deployment"
	Name ::     ID
	spec replicas: *1 | int
}

configMap <ID>: {
	metadata name: ID
	metadata labels component: Component
}

_spec: {
	Name :: string

	metadata name: Name
	metadata labels component: Component
	spec template: {
		metadata labels: {
			app:       Name
			component: Component
			domain:    "prod"
		}
		spec containers: [{name: Name}]
	}
}

// Define the _export option and set the default to true
// for all ports defined in all containers.
_spec spec template spec containers: [...{
	ports: [...{
		_export: *true | false // include the port in the service
	}]
}]

for x in [deployment, daemonSet, statefulSet] for k, v in x {
	service "\(k)": {
		spec selector: v.spec.template.metadata.labels

		spec ports: [ {
			Port = p.containerPort // Port is an alias
			port:       *Port | int
			targetPort: *Port | int
		} for c in v.spec.template.spec.containers
			for p in c.ports
			if p._export ]
	}
}
