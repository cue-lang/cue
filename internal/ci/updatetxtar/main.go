// Copyright 2021 The CUE Authors
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

package main

import (
	"flag"
	"io"
	"io/ioutil"
	"log"
	"os"

	"github.com/rogpeppe/go-internal/txtar"
)

// Usage:
//    updateTxtar source target filename
//
// updateTxtar writes the contents of source (could be - for stdin) to a file
// (identified by filename) within the txtar archive at target.

func main() {
	log.SetFlags(0)
	flag.Parse()
	if flag.NArg() != 3 {
		log.Fatal("Usage:\n\tupdateTxtar source target filename")
	}
	source := flag.Arg(0)
	target := flag.Arg(1)
	fn := flag.Arg(2)
	a, err := txtar.ParseFile(target)
	if err != nil {
		log.Fatal(err)
	}
	var file *txtar.File
	for i, f := range a.Files {
		if f.Name == fn {
			file = &a.Files[i]
			break
		}
	}
	if file == nil {
		a.Files = append(a.Files, txtar.File{Name: fn})
		file = &a.Files[len(a.Files)-1]
	}
	var sourceReader io.Reader
	if source == "-" {
		sourceReader = os.Stdin
	} else {
		sourceReader, err = os.Open(source)
		if err != nil {
			log.Fatal(err)
		}
	}
	contents, err := ioutil.ReadAll(sourceReader)
	if err != nil {
		log.Fatal(err)
	}
	file.Data = contents
	if err := ioutil.WriteFile(target, txtar.Format(a), 0666); err != nil {
		log.Fatal(err)
	}
}
