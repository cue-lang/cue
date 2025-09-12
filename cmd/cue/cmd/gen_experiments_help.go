//go:build ignore

// Copyright 2025 CUE Authors
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

// This tool generates experimentsHelp command for the help system based on
// experiments defined in internal/cueexperiment/file.go

package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"log"
	"net/url"
	"os"
	"reflect"
	"regexp"
	"sort"
	"strings"

	"cuelang.org/go/internal/cueexperiment"
	"cuelang.org/go/internal/mod/semver"
)

type Experiment struct {
	Name      string
	FieldName string
	Preview   string
	Stable    string
	Withdrawn string
	Comment   string
}

func main() {
	experiments, err := extractExperiments()
	if err != nil {
		log.Fatalf("Failed to extract experiments: %v", err)
	}

	// Filter experiments from v0.14.0 onwards
	var filtered []Experiment
	for _, exp := range experiments {
		if exp.Preview != "" && semver.Compare(exp.Preview, "v0.14.0") >= 0 {
			filtered = append(filtered, exp)
		}
	}

	// Sort experiments by preview version, then by name
	sort.Slice(filtered, func(i, j int) bool {
		if filtered[i].Preview != filtered[j].Preview {
			return semver.Compare(filtered[i].Preview, filtered[j].Preview) < 0
		}
		return filtered[i].Name < filtered[j].Name
	})

	// Validate URLs in comments
	validateURLsInComments(filtered)

	output := generateHelpCommand(filtered)

	if err := os.WriteFile("experiments_help_gen.go", []byte(output), 0644); err != nil {
		log.Fatalf("Failed to write generated file: %v", err)
	}
}

func extractExperiments() ([]Experiment, error) {
	// Parse the cueexperiment/file.go to extract comments
	fset := token.NewFileSet()
	src := "../../../internal/cueexperiment/file.go"
	f, err := parser.ParseFile(fset, src, nil, parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("failed to parse file.go: %w", err)
	}

	// Map field names to their comments and metadata
	fieldComments := make(map[string]*fieldInfo)

	// Find the File struct
	for _, decl := range f.Decls {
		genDecl, ok := decl.(*ast.GenDecl)
		if !ok || genDecl.Tok != token.TYPE {
			continue
		}

		for _, spec := range genDecl.Specs {
			typeSpec, ok := spec.(*ast.TypeSpec)
			if !ok || typeSpec.Name.Name != "File" {
				continue
			}

			structType, ok := typeSpec.Type.(*ast.StructType)
			if !ok {
				continue
			}

			// Extract field information
			for _, field := range structType.Fields.List {
				if len(field.Names) == 0 {
					continue
				}
				fieldName := field.Names[0].Name

				info := &fieldInfo{}
				if field.Comment != nil {
					info.comment = extractFieldComment(field.Comment.List)
				}
				if field.Doc != nil {
					info.comment = extractFieldComment(field.Doc.List)
				}

				fieldComments[fieldName] = info
			}
		}
	}

	// Use reflection to get experiment info from the actual struct
	var experiments []Experiment
	fileType := reflect.TypeOf(cueexperiment.File{})

	for i := 0; i < fileType.NumField(); i++ {
		field := fileType.Field(i)
		tagStr, ok := field.Tag.Lookup("experiment")
		if !ok {
			continue
		}

		expInfo := parseExperimentTag(tagStr)
		if expInfo == nil {
			continue
		}

		fieldInfo := fieldComments[field.Name]
		comment := ""

		if fieldInfo != nil {
			comment = fieldInfo.comment
		}

		exp := Experiment{
			Name:      strings.ToLower(field.Name),
			FieldName: field.Name,
			Preview:   expInfo.Preview,
			Stable:    expInfo.Stable,
			Withdrawn: expInfo.Withdrawn,
			Comment:   comment,
		}

		experiments = append(experiments, exp)
	}

	return experiments, nil
}

type fieldInfo struct {
	comment string
}

type experimentInfo struct {
	Preview   string
	Stable    string
	Withdrawn string
}

func extractFieldComment(comments []*ast.Comment) string {
	var lines []string
	for _, comment := range comments {
		text := strings.TrimPrefix(comment.Text, "//")
		text = strings.TrimSpace(text)
		if text != "" {
			lines = append(lines, text)
		}
	}
	return strings.Join(lines, "\n")
}

func parseExperimentTag(tagStr string) *experimentInfo {
	info := &experimentInfo{}
	for _, part := range strings.Split(tagStr, ",") {
		part = strings.TrimSpace(part)
		key, value, found := strings.Cut(part, ":")
		if !found {
			continue
		}
		switch key {
		case "preview":
			info.Preview = value
		case "stable":
			info.Stable = value
		case "withdrawn":
			info.Withdrawn = value
		}
	}
	return info
}

func generateHelpCommand(experiments []Experiment) string {
	var sb strings.Builder

	sb.WriteString(`// Copyright 2025 CUE Authors
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

// Code generated by gen_experiments_help.go; DO NOT EDIT.

package cmd

import "github.com/spf13/cobra"

var experimentsHelp = &cobra.Command{
	Use:   "experiments",
	Short: "experimental language features",
	Long: `)
	sb.WriteString("`")
	sb.WriteString(`
Experimental language features that can be enabled on a per-file basis
using the @experiment attribute.

Experiments are enabled in CUE files using file-level attributes:

	@experiment(structcmp)
	
	package mypackage
	
	// experiment is now active for this file

Multiple experiments can be enabled:

	@experiment(structcmp,self)
	@experiment(explicitopen)

Available experiments:

`)

	for _, exp := range experiments {
		sb.WriteString(fmt.Sprintf("  %s (preview: %s", exp.Name, exp.Preview))
		if exp.Stable != "" {
			sb.WriteString(fmt.Sprintf(", stable: %s", exp.Stable))
		}
		sb.WriteString(")\n")

		// Add full comment if available
		if exp.Comment != "" {
			// Split into lines and indent each line
			lines := strings.Split(exp.Comment, "\n")
			for _, line := range lines {
				line = strings.TrimSpace(line)
				if line != "" {
					// Replace field name with lowercase version in the first occurrence
					line = strings.Replace(line, exp.FieldName, exp.Name, 1)
					// Escape backticks to avoid syntax errors in Go string literals
					line = strings.ReplaceAll(line, "`", "`+\"`\"+`")
					sb.WriteString(fmt.Sprintf("    %s\n", line))
				}
			}
		}

		sb.WriteString("\n")
	}

	sb.WriteString(`Language experiments may change behavior, syntax, or semantics.
Use with caution in production code.
`)
	sb.WriteString("`")
	sb.WriteString("[1:],\n}\n")

	return sb.String()
}

// validateURLsInComments checks that all URLs found in experiment comments are valid
func validateURLsInComments(experiments []Experiment) {
	urlPattern := regexp.MustCompile(`https://[^\s]+`)

	for _, exp := range experiments {
		if exp.Comment == "" {
			continue
		}

		urls := urlPattern.FindAllString(exp.Comment, -1)
		for _, rawURL := range urls {
			// Parse the URL to validate it
			parsedURL, err := url.Parse(rawURL)
			if err != nil {
				log.Printf("WARNING: Invalid URL in %s experiment: %s (error: %v)", exp.Name, rawURL, err)
				continue
			}

			// Basic validation checks
			if parsedURL.Scheme != "https" {
				log.Printf("WARNING: Non-HTTPS URL in %s experiment: %s", exp.Name, rawURL)
			}

			// Check for expected CUE domains
			expectedDomains := []string{"cuelang.org"}
			isExpectedDomain := false
			for _, domain := range expectedDomains {
				if strings.Contains(parsedURL.Host, domain) {
					isExpectedDomain = true
					break
				}
			}

			if !isExpectedDomain {
				log.Printf("INFO: Unexpected domain in %s experiment: %s", exp.Name, rawURL)
			}
		}
	}
}
