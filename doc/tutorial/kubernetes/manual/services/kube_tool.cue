package kube

objects: [ for v in objectSets for x in v { x } ]

objectSets: [
	kubernetes.services,
	kubernetes.deployments,
	kubernetes.statefulSets,
	kubernetes.daemonSets,
	kubernetes.configMaps,
]
