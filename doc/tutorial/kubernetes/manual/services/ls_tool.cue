package kube

import "strings"

command ls: {
    task print: {
        kind: "print"
        Lines = [
            "\(x.kind)  \t\(x.metadata.labels.component)   \t\(x.metadata.name)"
            for x in objects ]
        text: strings.Join(Lines, "\n")
    }
}
