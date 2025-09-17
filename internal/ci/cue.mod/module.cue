module: "github.com/cue-lang/tmp/internal/ci"
language: {
	version: "v0.13.0"
}
source: {
	kind: "self"
}
deps: {
	"cue.dev/x/githubactions@v0": {
		v:       "v0.2.0"
		default: true
	}
}
