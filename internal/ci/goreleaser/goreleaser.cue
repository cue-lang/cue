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
}
