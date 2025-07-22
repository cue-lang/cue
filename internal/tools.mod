// This module exists just so that we can track extra tooling dependencies
// to be used via `go tool` without polluting the main go.mod file.
// TODO(mvdan): once we stabilize on this model, have CI ensure this module is tidy too.
module test/tools

go 1.23.0

tool honnef.co/go/tools/cmd/staticcheck

require (
	github.com/BurntSushi/toml v1.4.1-0.20240526193622-a339e1f7089c // indirect
	golang.org/x/exp/typeparams v0.0.0-20231108232855-2478ac86f678 // indirect
	golang.org/x/mod v0.23.0 // indirect
	golang.org/x/sync v0.11.0 // indirect
	golang.org/x/tools v0.30.0 // indirect
	honnef.co/go/tools v0.6.1 // indirect
)
