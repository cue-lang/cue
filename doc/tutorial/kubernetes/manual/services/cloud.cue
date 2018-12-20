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
	name:     Name | string
	kind:     "deployment" | "stateful" | "daemon"
	replicas: 1 | int

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
	envSpec: {"\(k)" value: v for k, v in env}

	volume <Name>: {
		name:      Name | string
		mountPath: string
		subPath:   null | string
		readOnly:  false | true
		kubernetes: {}
	}
}

service <Name>: _base & {
	name: Name | string

	port <Name>: {
		name: Name | string

		port:       int
		targetPort: port | int
		protocol:   "TCP" | "UDP"
	}

	kubernetes: {}
}

configMap <Name>: {
}

// define services implied by deployments
service "\(k)": {

	// Copy over all ports exposed from containers.
	port "\(Name)": {
		port:       Port | int
		targetPort: Port | int
	} for Name, Port in spec.expose.port

	// Copy over the labels
	label: spec.label

} for k, spec in deployment if len(spec.expose.port) > 0
