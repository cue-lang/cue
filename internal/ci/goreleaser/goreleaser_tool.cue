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

	let _githubRef = env.GITHUB_REF | "refs/no_ref_kind/not_a_release" // filled when running in CI
	let _githubRefName = path.Base(_githubRef)

	// Only run the full release when running on GitHub actions for a release tag.
	// Keep in sync with core.#releaseTagPattern, which is a globbing pattern
	// rather than a regular expression.
	let snapshot = [ if _githubRef !~ "refs/tags/v.*" {"--snapshot"}, "" ][0]

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
		cmd: ["go", "list", "-m", "-f", "{{.Version}}", "cuelang.org/go@latest"]
		stdout: string
	}

	let latestCUEVersion = strings.TrimSpace(latestCUE.stdout)

	tidyUp: os.RemoveAll & {
		$after: latestCUE
		path:   tempDir.path
	}

	cueModRoot: exec.Run & {
		cmd: ["go", "list", "-m", "-f", "{{.Dir}}", "cuelang.org/go"]
		stdout: string
	}

	info: cli.Print & {
		text: """
			snapshot: \(snapshot)
			latest CUE version: \(latestCUEVersion)
			git ref: \(_githubRef)
			release name: \(_githubRefName)
			"""
	}

	goreleaser: exec.Run & {
		$after: info

		// Set the goreleaser configuration to be stdin
		stdin: yaml.Marshal(config & {
			#latest: _githubRefName == strings.TrimSpace(latestCUE.stdout)
		})

		// Run at the root of the module
		dir: strings.TrimSpace(cueModRoot.stdout)

		cmd: ["goreleaser", "release", "-f", "-", "--rm-dist", snapshot]
	}
}
