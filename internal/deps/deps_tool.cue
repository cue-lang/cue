package deps

import (
	"tool/http"
	"tool/exec"
	"strings"
)

command: vendorgithubschema: {
	get: http.Get & {
		request: body: ""
		url: "https://raw.githubusercontent.com/SchemaStore/schemastore/f7a0789ccb3bd74a720ddbd6691d60fd9e2d8b7a/src/schemas/json/github-workflow.json"
	}
	root: exec.Run & {
		cmd:    "go list -m -f {{.Dir}}"
		stdout: string
	}
	convert: exec.Run & {
		stdin: get.response.body
		cmd:   "go run cuelang.org/go/cmd/cue import -f -p json -l #Workflow: jsonschema: - --outfile \(strings.TrimSpace(root.stdout))/cue.mod/pkg/github.com/SchemaStore/schemastore/src/schemas/json/github-workflow.cue"
	}
}
