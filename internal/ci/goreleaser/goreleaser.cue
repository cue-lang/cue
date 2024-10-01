package goreleaser

config: {
	#latest: bool @tag(latest, type=bool)

	version:      2
	project_name: "cue"
	// Note that gomod.proxy is ignored by `goreleaser release --snapshot`,
	// which we use in CI to test the goreleaser config and build,
	// as --snapshot is meant for entirely local builds without a git tag.
	gomod: proxy: true

	// Template based on common settings
	builds: [...{
		env: *[
			"CGO_ENABLED=0",
		] | _
		ldflags: *[
			"-s -w",
		] | _
		flags: *[
			"-trimpath",
		] | _
		// Note that goreleaser says that id defaults to the binary name,
		// but it then complains about "cue" being duplicate even though we use "cue" and "cuepls".
		id:            binary
		main:          string
		binary:        string
		mod_timestamp: *'{{ .CommitTimestamp }}' | _
		goos: *[
			"darwin",
			"linux",
			"windows",
		] | _
		goarch: *[
			"amd64",
			"arm64",
		] | _
	}]

	builds: [
		{main: "./cmd/cue", binary: "cue"},
		{main: "./cmd/cuepls", binary: "cuepls"},
	]

	archives: [{
		name_template: "{{ .ProjectName }}_{{ .Tag }}_{{ .Os }}_{{ .Arch }}{{ if .Arm }}v{{ .Arm }}{{ end }}{{ if .Mips }}_{{ .Mips }}{{ end }}"
		files: [
			"LICENSE",
			"README.md",
			"doc/tutorial/**/*",
			"doc/ref/spec.md",
		]
		format_overrides: [{
			goos:   "windows"
			format: "zip"
		}]
	}]
	release: {
		disable:    false
		prerelease: "auto"

		// We manually write the release notes, so they need to be added to a release on GitHub.
		// We don't want to create the release from scratch without goreleaser,
		// since goreleaser takes care of creating and uploading the release archives.
		// We also don't want the release to be fully published by goreleaser,
		// as otherwise the notification emails go out with the release notes missing.
		// For those reasons, let goreleaser create the release, but leaving it as a draft.
		draft: true
	}
	checksum: name_template:    "checksums.txt"
	snapshot: version_template: "{{ .Tag }}-next"
	// As explained above, we write our own release notes.
	changelog: disable: true

	brews: [{
		if !#latest {
			skip_upload: true
		}
		repository: {
			owner: "cue-lang"
			name:  "homebrew-tap"
		}
		commit_author: {
			name:  "cueckoo"
			email: "noreply@cuelang.org"
		}
		homepage:    "https://cuelang.org"
		description: "CUE is an open source data constraint language which aims to simplify tasks involving defining and using data."
		test: """
			system \"#{bin}/cue version\"

			"""
	}]

	dockers: [{
		image_templates: [
			"docker.io/cuelang/cue:{{ .Version }}-amd64",
		]
		dockerfile: "cmd/cue/Dockerfile"
		use:        "buildx"
		build_flag_templates: [
			"--platform=linux/amd64",
			"--label=org.opencontainers.image.title={{ .ProjectName }}",
			"--label=org.opencontainers.image.description={{ .ProjectName }}",
			"--label=org.opencontainers.image.url=https://github.com/cue-lang/cue",
			"--label=org.opencontainers.image.source=https://github.com/cue-lang/cue",
			"--label=org.opencontainers.image.version={{ .Version }}",
			"--label=org.opencontainers.image.created={{ time \"2006-01-02T15:04:05Z07:00\" }}",
			"--label=org.opencontainers.image.revision={{ .FullCommit }}",
			"--label=org.opencontainers.image.licenses=Apache 2.0",
		]
	}, {
		image_templates: [
			"docker.io/cuelang/cue:{{ .Version }}-arm64",
		]
		goarch:     "arm64"
		dockerfile: "cmd/cue/Dockerfile"
		use:        "buildx"
		build_flag_templates: [
			"--platform=linux/arm64",
			"--label=org.opencontainers.image.title={{ .ProjectName }}",
			"--label=org.opencontainers.image.description={{ .ProjectName }}",
			"--label=org.opencontainers.image.url=https://github.com/cue-lang/cue",
			"--label=org.opencontainers.image.source=https://github.com/cue-lang/cue",
			"--label=org.opencontainers.image.version={{ .Version }}",
			"--label=org.opencontainers.image.created={{ time \"2006-01-02T15:04:05Z07:00\" }}",
			"--label=org.opencontainers.image.revision={{ .FullCommit }}",
			"--label=org.opencontainers.image.licenses=Apache 2.0",
		]
	}]

	docker_manifests: [
		{
			name_template: "docker.io/cuelang/cue:{{ .Version }}"
			image_templates: [
				"docker.io/cuelang/cue:{{ .Version }}-amd64",
				"docker.io/cuelang/cue:{{ .Version }}-arm64",
			]
		},
		if #latest {
			name_template: "docker.io/cuelang/cue:latest"
			image_templates: [
				"docker.io/cuelang/cue:{{ .Version }}-amd64",
				"docker.io/cuelang/cue:{{ .Version }}-arm64",
			]
		},
	]
}
