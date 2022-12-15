package goreleaser

config: {
	#latest: bool @tag(latest, type=bool)

	project_name: "cue"
	gomod: proxy: true
	builds: [{
		env: [
			"CGO_ENABLED=0",
		]
		main:   "./cmd/cue"
		binary: "cue"
		ldflags: [
			"-s -w",
		]
		flags: [
			"-trimpath",
		]
		mod_timestamp: '{{ .CommitTimestamp }}'
		goos: [
			"darwin",
			"linux",
			"windows",
		]
		goarch: [
			"amd64",
			"arm64",
		]
	}]
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
	}
	checksum: name_template: "checksums.txt"
	snapshot: name_template: "{{ .Tag }}-next"
	changelog: {
		sort: "asc"
		filters: exclude: [
			"^test:",
		]
	}

	brews: [{
		if !#latest {
			skip_upload: true
		}
		tap: {
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
		dockerfile: "Dockerfile"
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
		dockerfile: "Dockerfile"
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
