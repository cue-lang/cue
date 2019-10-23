package kube

import "encoding/yaml"

command: dump: {
	task: print: {
		kind: "print"
		text: yaml.MarshalStream(objects)
	}
}
