package workflows

import (
	"github.com/SchemaStore/schemastore/src/schemas/json"
)

_#goGenerate: json.#step & {
	name: "Generate"
	run:  "go generate ./..."
	// The Go version corresponds to the precise version specified in
	// the matrix. Skip windows for now until we work out why re-gen is flaky
	if: "matrix.go-version == '\(_#latestStableGo)' && matrix.os == '\(_#linuxMachine)'"
}

_#goTest: json.#step & {
	name: "Test"
	run:  "go test ./..."
}

_#goCheck: json.#step & {
	// These checks can vary between platforms, as different code can be built
	// based on GOOS and GOARCH build tags.
	// However, CUE does not have any such build tags yet, and we don't use
	// dependencies that vary wildly between platforms.
	// For now, to save CI resources, just run the checks on one matrix job.
	// TODO: consider adding more checks as per https://github.com/golang/go/issues/42119.
	if:   "matrix.go-version == '\(_#latestStableGo)' && matrix.os == '\(_#linuxMachine)'"
	name: "Check"
	run:  "go vet ./..."
}

_#goTestRace: json.#step & {
	name: "Test with -race"
	run:  "go test -race ./..."
}
