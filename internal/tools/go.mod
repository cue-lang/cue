// This module exists just so that we can track extra tooling dependencies
// to be used via `go tool` without polluting the main go.mod file.
// It holds no packages; the directory to itself is what makes `go mod tidy`
// treat this file as the module root rather than the repository root.
module test/tools

go 1.26.0

tool honnef.co/go/tools/cmd/staticcheck

require (
	github.com/BurntSushi/toml v1.4.1-0.20240526193622-a339e1f7089c // indirect
	golang.org/x/exp/typeparams v0.0.0-20231108232855-2478ac86f678 // indirect
	golang.org/x/mod v0.41.0 // indirect
	golang.org/x/sync v0.23.0 // indirect
	golang.org/x/tools v0.51.0 // indirect
	honnef.co/go/tools v0.8.1 // indirect
)
