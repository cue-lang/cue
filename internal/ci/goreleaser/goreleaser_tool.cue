package goreleaser

import (
	"path"
	"strings"
	"encoding/yaml"

	"tool/file"
	"tool/exec"
	"tool/os"
	"tool/cli"
)

command: release: {
	env: os.Environ

	let _env = env
	let _githubActions = env.GITHUB_ACTIONS | "" // "true" if running in CI
	let _githubRef = path.Base(env.GITHUB_REF | "refs/tags/<not a github release>")

	// Only run the full release as part of GitHub actions
	let snapshot = [ if _githubActions != "true" {"--snapshot"}, ""][0]

	tempDir: file.MkdirTemp & {
		path: string
	}

	goMod: file.Create & {
		contents: "module mod.test"
		filename: path.Join([tempDir.path, "go.mod"])
	}

	latestCUE: exec.Run & {
		env: {
			_env
			GOPROXY: "direct" // skip proxy.golang.org in case its @latest is lagging behind
		}
		$after: goMod
		dir:    tempDir.path
		cmd:    ["go", "list", "-m", "-f", "{{.Version}}", "cuelang.org/go@latest"]
		stdout: string
	}

	let latestCUEVersion = strings.TrimSpace(latestCUE.stdout)

	tidyUp: os.RemoveAll & {
		$after: latestCUE
		path:   tempDir.path
	}

	cueModRoot: exec.Run & {
		cmd:    ["go", "list", "-m", "-f", "{{.Dir}}", "cuelang.org/go"]
		stdout: string
	}

	info: cli.Print & {
		text: """
			snapshot: \(snapshot)
			latest CUE version: \(latestCUEVersion)
			release version: \(_githubRef)
			"""
	}

	goreleaser: exec.Run & {
		$after: info

		// Set the goreleaser configuration to be stdin
		stdin: yaml.Marshal(config & {
			#latest: path.Base(_githubRef) == strings.TrimSpace(latestCUE.stdout)
		})

		// Run at the root of the module
		dir: strings.TrimSpace(cueModRoot.stdout)

		cmd: ["goreleaser", "release", "-f", "-", "--rm-dist", snapshot]
	}
}
