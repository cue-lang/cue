# Getting Started

Currently CUE can only be installed from source.

## Install CUE from source

### Prerequisites

Go 1.16 or higher (see below)

### Installing CUE

<!-- Keep the following in sync with cmd/cue/cmd/testdata/script/install*.txt -->

To download and install the `cue` command line tool run

```
go install cuelang.org/go/cmd/cue@latest
```

If the command fails, make sure your version of Go is 1.16 or later.

And make sure the install directory is in your path.

To also download the API and documentation, run

```
go get cuelang.org/go/cue@latest
```

in a module context.


### Installing Go

#### Download Go

You can install binaries for Windows, MacOS X, and Linux at https://golang.org/dl/. If you use a different OS you can
[install Go from source](https://golang.org/doc/install/source).

#### Install Go

Follow the instructions at https://golang.org/doc/install#install.
Make sure the `go` binary is in your path.
CUE uses Go modules, so there is no need to set up a GOPATH.
