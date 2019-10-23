package kube

deployment: linecook: _kitchenDeployment & {
	image: "gcr.io/myproj/linecook:v0.1.42"
	volume: "secret-linecook": name: "secret-kitchen"

	arg: name:                "linecook"
	arg: etcd:                "etcd:2379"
	arg: "reconnect-delay":   "1h"
	arg: "-recovery-overlap": "100000"
}
