package kube

import (
	"encoding/yaml"
	"tool/exec"
	"tool/cli"
)

command create: {
	task kube: exec.Run & {
		cmd:    "kubectl create --dry-run -f -"
		stdin:  yaml.MarshalStream(objects)
		stdout: string
	}

	task display: cli.Print & {
		text: task.kube.stdout
	}
}
