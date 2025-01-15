// Copyright 2025 The CUE Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// gomod-txtar toggles testscript-based txtar files between a form that uses a
// main Go module to run cmd/cue and a form that uses cue from PATH.
//
//	gomod-txtar [files]
package main

import (
	"bytes"
	_ "embed"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"regexp"
	"slices"

	"golang.org/x/tools/txtar"
)

func main() {
	flag.Usage = func() {
		fmt.Fprint(flag.CommandLine.Output(), `
usage of gomod-txtar:

  gomod-txtar [files]

	 Toggle testscript-based txtar files between a form that uses:

	  	exec cue

	 and one that uses:

	  	exec go run cuelang.org/go/cmd/cue

	 adding/removing the associated Go module machinery as required.

	 If no files are provided as arguments, or if '-' is explicitly provided as
	 the sole file argument, stdin is read and the toggled output is written to
	 stdout.

	 The environment variable CUE_DEV must be set, and provides the directory
	 replace location of the cuelang.org/go module.
`[1:])
	}
	flag.Parse()

	// Ensure that CUE_DEV is set
	replaceTarget := os.Getenv("CUE_DEV")
	if replaceTarget == "" {
		log.Fatalf("CUE_DEV environment variable is not set")
	}

	// Check file args
	args := flag.Args()
	if slices.Contains(args, "-") && len(args) > 1 {
		log.Fatal("file '-' must appear as only file argument")
	}
	files := args
	if len(files) == 0 {
		files = []string{"-"}
	}

	for _, fileName := range files {
		var contents []byte
		var err error

		if fileName == "-" {
			contents, err = io.ReadAll(os.Stdin)
		} else {
			contents, err = os.ReadFile(fileName)
		}
		if err != nil {
			log.Fatal(err)
		}

		ar := txtar.Parse(contents)
		toggleArchive(ar, replaceTarget)
		res := txtar.Format(ar)

		if fileName == "-" {
			os.Stdout.Write(res)
		} else {
			err = os.WriteFile(fileName, res, 0x666)
		}
		if err != nil {
			log.Fatal(err)
		}
	}
}

//go:embed prefix.txtar
var prefixTemplate string

const (
	goModCmd = "exec go run cuelang.org/go/cmd/cue"
	cueCmd   = "exec cue"
)

var (
	goModCmdLinePrefix = buildLinePrefix(goModCmd)
	cueCmdLinePrefix   = buildLinePrefix(cueCmd)
)

func buildLinePrefix(cmd string) *regexp.Regexp {
	return regexp.MustCompile("(?m)^" + cmd + " ")
}

// toggleArchive toggles the Go module-ness of ar, using replaceDir as the
// directory replace target of cuelang.org/go.
func toggleArchive(ar *txtar.Archive, replaceDir string) {

	// Be conservative in our detecting of the signature of the Go module-ness
	// of an archive. Require that the prefix of the comment section matches
	// exactly, and that the first files are identical.

	prefixBytes := []byte(fmt.Sprintf(prefixTemplate, replaceDir))
	prefix := txtar.Parse(prefixBytes)

	var linePrefixRegexp *regexp.Regexp
	var linePrefixReplacement string

	if archiveHasPrefix(ar, prefix) {
		ar.Comment = bytes.TrimPrefix(ar.Comment, prefix.Comment)
		ar.Files = ar.Files[len(prefix.Files):]
		linePrefixRegexp = goModCmdLinePrefix
		linePrefixReplacement = cueCmd
	} else {
		ar.Comment = slices.Concat(prefix.Comment, ar.Comment)
		ar.Files = slices.Concat(prefix.Files, ar.Files)
		linePrefixRegexp = cueCmdLinePrefix
		linePrefixReplacement = goModCmd
	}

	// Add trailing ' '
	linePrefixReplacement += " "

	ar.Comment = linePrefixRegexp.ReplaceAll(ar.Comment, []byte(linePrefixReplacement))
}

func archiveHasPrefix(ar *txtar.Archive, p *txtar.Archive) bool {
	if !bytes.HasPrefix(ar.Comment, p.Comment) {
		return false
	}
	if len(ar.Files) < 2 {
		return false
	}
	for i, pf := range p.Files {
		arf := ar.Files[i]
		if arf.Name != pf.Name {
			return false
		}
		if !bytes.Equal(arf.Data, pf.Data) {
			return false
		}
	}
	return true
}
