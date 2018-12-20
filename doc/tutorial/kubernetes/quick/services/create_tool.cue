package kube

import "encoding/yaml"

command create: {
    task kube: {
        kind:   "exec"
        cmd:    "kubectl create --dry-run -f -"
        stdin:  yaml.MarshalStream(objects)
        stdout: string
    }
    task display: {
        kind: "print"
        text: task.kube.stdout
    }
}
