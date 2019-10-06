// Copyright 2019 CUE Authors
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

// +build ignore

// gen generates Hugo files from the given current script files.
package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"text/template"

	"github.com/rogpeppe/testscript/txtar"
)

func main() {
	log.SetFlags(log.Lshortfile)
	index := ""
	filepath.Walk(".", func(path string, info os.FileInfo, err error) error {
		if x, ok := chapter[path]; ok {
			index = x
		}
		if !strings.HasSuffix(path, ".txt") ||
			filepath.Base(path) == "out.txt" {
			return nil
		}
		generate(path, index)
		return nil
	})
}

var chapter = map[string]string{
	"intro":       "0",
	"types":       "2",
	"references":  "4",
	"expressions": "6",
	"packages":    "8",
}

type Page struct {
	FrontMatter string
	Weight      int
	Body        string
	Command     string
	Inputs      []File
	Out         File
}

type File struct {
	Name string
	Data string
	Type string
}

var hugoPage = template.Must(template.New("page").Delims("[[", "]]").Parse(`+++
[[ .FrontMatter ]]
weight = [[ .Weight ]]
layout = "tutorial"
+++
[[.Body -]]
[[- if .Inputs ]]
<a id="td-block-padding" class="td-offset-anchor"></a>
<section class="row td-box td-box--white td-box--gradient td-box--height-auto">
<div class="col-lg-6 mr-0">
[[ range .Inputs -]]
<i>[[ .Name ]]</i>
<p>
{{< highlight go >}}
[[ .Data -]]
{{< /highlight >}}
<br>
[[end -]]
</div>

<div class="col-lg-6 ml-0">
[[- if .Out.Data -]]
<i>$ [[ .Command ]]</i>
<p>
{{< highlight go >}}
[[ .Out.Data -]]
{{< /highlight >}}
[[end -]]
</div>
</section>
[[- end -]]
`))

func generate(filename, index string) {
	a, err := txtar.ParseFile(filename)
	if err != nil {
		log.Fatal(err)
	}

	re := regexp.MustCompile(`(\d+)_`)
	for _, m := range re.FindAllStringSubmatch(filename, 2) {
		index += m[1]
	}
	weight, err := strconv.Atoi(index)
	if err != nil {
		log.Fatal(err)
	}
	filename = re.ReplaceAllLiteralString(filename, "")
	filename = filename[:len(filename)-len(".txt")] + ".md"
	filename = filepath.Join("tour", filename)
	fmt.Println(index, filename)

	comments := strings.Split(string(a.Comment), "\n")[0]
	comments = strings.TrimLeft(comments, "! ")
	page := &Page{
		Command: comments,
		Weight:  2000 + weight,
	}

	for _, f := range a.Files {
		data := string(f.Data)
		file := File{Name: f.Name, Data: data}

		switch s := f.Name; {
		case s == "frontmatter.toml":
			page.FrontMatter = strings.TrimSpace(data)

		case strings.HasSuffix(s, ".md"):
			page.Body = data

		case strings.HasSuffix(s, ".cue"):
			file.Type = "cue"
			page.Inputs = append(page.Inputs, file)

		case strings.HasSuffix(s, ".json"):
			file.Type = "json"
			page.Inputs = append(page.Inputs, file)

		case strings.HasSuffix(s, ".yaml"):
			file.Type = "yaml"
			page.Inputs = append(page.Inputs, file)

		case strings.HasSuffix(s, "stdout-cue"):
			file.Type = "cue"
			page.Out = file

		case strings.HasSuffix(s, "stdout-json"):
			file.Type = "json"
			page.Out = file

		case strings.HasSuffix(s, "stderr"):
			page.Out = file

		default:
			log.Fatalf("unknown file type %q", s)
		}
	}

	_ = os.MkdirAll(filepath.Dir(filename), 0755)

	w, err := os.Create(filename)
	if err != nil {
		log.Fatal(err)
	}
	defer w.Close()

	if err = hugoPage.Execute(w, page); err != nil {
		log.Fatal(err)
	}
}
