package kube

objects: [ x for v in objectSets for x in v ]

objectSets: [
	kubernetes.services,
	kubernetes.deployments,
	kubernetes.statefulSets,
	kubernetes.daemonSets,
	kubernetes.configMaps,
]
