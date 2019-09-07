package kube

// _base defines settings that apply to all cloud objects
_base: {
	name: string

	label <Key>: string

	// k8s is a set of Kubernetes-specific settings that will be merged in at
	// the top-level. The allowed fields are type specfic.
	kubernetes: {}
}

deployment <Name>: _base & {
	// Allow any string, but take Name by default.
	name:     string | *Name
	kind:     *"deployment" | "stateful" | "daemon"
	replicas: int | *1

	image: string

	// expose port defines named ports that is exposed in the service
	expose port <N>: int

	// port defines named ports that is not exposed in the service.
	port <N>: int

	arg <Key>: string
	args: [ "-\(k)=\(v)" for k, v in arg ] | [...string]

	// Environment variables
	env <Key>: string

	envSpec <Key>: {}
	envSpec: {
		for k, v in env {
			"\(k)" value: v
		}
	}

	volume <Name>: {
		name:      string | *Name
		mountPath: string
		subPath:   string | *null
		readOnly:  *false | true
		kubernetes: {}
	}
}

service <Name>: _base & {
	name: *Name | string

	port <Name>: {
		name: string | *Name

		port:     int
		protocol: *"TCP" | "UDP"
	}

	kubernetes: {}
}

configMap <Name>: {
}

// define services implied by deployments
for k, spec in deployment if len(spec.expose.port) > 0 {
	service "\(k)": {

		// Copy over all ports exposed from containers.
		for Name, Port in spec.expose.port {
			port "\(Name)": {
				// Set default external port to Port. targetPort must be
				// the respective containerPort (Port) if it differs from port.
				port: int | *Port
				if port != Port {
					targetPort: Port
				}
			}
		}

		// Copy over the labels
		label: spec.label
	}
}
