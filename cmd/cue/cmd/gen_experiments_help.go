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
	Default   string
	Stable    string
	Withdrawn string
	Comment   string
	IsGlobal  bool // true for CUE_EXPERIMENT, false for @experiment
}

func main() {
	experiments, err := extractExperiments()
	if err != nil {
		log.Fatalf("Failed to extract experiments: %v", err)
	}

	// Separate per-file and global experiments
	var fileExperiments []Experiment
	var globalExperiments []Experiment

	for _, exp := range experiments {
		if exp.IsGlobal {
			globalExperiments = append(globalExperiments, exp)
		} else {
			// Filter file experiments from v0.14.0 onwards
			if exp.Preview != "" && semver.Compare(exp.Preview, "v0.14.0") >= 0 {
				fileExperiments = append(fileExperiments, exp)
			}
		}
	}

	// Sort file experiments by preview version, then by name
	sort.Slice(fileExperiments, func(i, j int) bool {
		if fileExperiments[i].Preview != fileExperiments[j].Preview {
			return semver.Compare(fileExperiments[i].Preview, fileExperiments[j].Preview) < 0
		}
		return fileExperiments[i].Name < fileExperiments[j].Name
	})

	// Sort global experiments by name
	sort.Slice(globalExperiments, func(i, j int) bool {
		return globalExperiments[i].Name < globalExperiments[j].Name
	})

	// Validate URLs in comments for all experiments
	allExperiments := append(fileExperiments, globalExperiments...)
	validateURLsInComments(allExperiments)

	output := generateHelpCommand(fileExperiments, globalExperiments)

	if err := os.WriteFile("experiments_help_gen.go", []byte(output), 0644); err != nil {
		log.Fatalf("Failed to write generated file: %v", err)
	}
}

func extractExperiments() ([]Experiment, error) {
	// Extract file experiments from File struct
	fileExperiments, err := extractExperimentsFromStruct(
		reflect.TypeOf(cueexperiment.File{}),
		"../../../internal/cueexperiment/file.go",
		"File",
		false, // IsGlobal = false for per-file experiments
	)
	if err != nil {
		return nil, fmt.Errorf("failed to extract file experiments: %w", err)
	}

	// Extract global experiments from Config struct
	globalExperiments, err := extractExperimentsFromStruct(
		reflect.TypeOf(cueexperiment.Config{}),
		"../../../internal/cueexperiment/exp.go",
		"Config",
		true, // IsGlobal = true for global experiments
	)
	if err != nil {
		return nil, fmt.Errorf("failed to extract global experiments: %w", err)
	}

	// Combine both types of experiments
	experiments := append(fileExperiments, globalExperiments...)
	return experiments, nil
}

type fieldInfo struct {
	comment string
}

type experimentInfo struct {
	Preview   string
	Default   string
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
		case "default":
			info.Default = value
		case "stable":
			info.Stable = value
		case "withdrawn":
			info.Withdrawn = value
		}
	}
	return info
}

func extractExperimentsFromStruct(structType reflect.Type, srcPath, structName string, isGlobal bool) ([]Experiment, error) {
	// Parse the source file to extract comments
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, srcPath, nil, parser.ParseComments)
	if err != nil {
		if isGlobal {
			log.Printf("Warning: failed to parse %s for comments: %v", srcPath, err)
			// For global experiments, continue without comments rather than failing
		} else {
			return nil, fmt.Errorf("failed to parse %s: %w", srcPath, err)
		}
	}

	// Map field names to their comments
	fieldComments := make(map[string]*fieldInfo)

	if f != nil {
		// Find the target struct
		for _, decl := range f.Decls {
			genDecl, ok := decl.(*ast.GenDecl)
			if !ok || genDecl.Tok != token.TYPE {
				continue
			}

			for _, spec := range genDecl.Specs {
				typeSpec, ok := spec.(*ast.TypeSpec)
				if !ok || typeSpec.Name.Name != structName {
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
	}

	// Use reflection to get experiment info from the struct
	var experiments []Experiment

	for i := 0; i < structType.NumField(); i++ {
		field := structType.Field(i)
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
			Default:   expInfo.Default,
			Stable:    expInfo.Stable,
			Withdrawn: expInfo.Withdrawn,
			Comment:   comment,
			IsGlobal:  isGlobal,
		}

		experiments = append(experiments, exp)
	}

	return experiments, nil
}

func generateHelpCommand(fileExperiments []Experiment, globalExperiments []Experiment) string {
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
Experimental features that can be enabled in CUE.

There are two types of experiments:

1. Per-file experiments: Enabled via @experiment attribute in CUE files
2. Global experiments: Enabled via CUE_EXPERIMENT environment variable

## Per-file Experiments

Experiments are enabled in CUE files using file-level attributes:

	@experiment(structcmp)

	package mypackage

	// experiment is now active for this file

Multiple experiments can be enabled:

	@experiment(structcmp,self)
	@experiment(explicitopen)

Available per-file experiments (v0.14.0 onwards):

`)

	// Generate per-file experiments
	for _, exp := range fileExperiments {
		sb.WriteString(fmt.Sprintf("  %s (preview: %s", exp.Name, exp.Preview))
		if exp.Default != "" {
			sb.WriteString(fmt.Sprintf(", default: %s", exp.Default))
		}
		if exp.Stable != "" {
			sb.WriteString(fmt.Sprintf(", stable: %s", exp.Stable))
		}
		if exp.Withdrawn != "" {
			sb.WriteString(fmt.Sprintf(", withdrawn: %s", exp.Withdrawn))
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

	// Add global experiments section
	if len(globalExperiments) > 0 {
		sb.WriteString(`
## Global Experiments

Global experiments are enabled via the CUE_EXPERIMENT environment variable:

	export CUE_EXPERIMENT=cmdreferencepkg,keepvalidators
	cue eval myfile.cue

Available global experiments:

`)

		for _, exp := range globalExperiments {
			sb.WriteString(fmt.Sprintf("  %s", exp.Name))
			if exp.Preview != "" {
				sb.WriteString(fmt.Sprintf(" (preview: %s", exp.Preview))
				if exp.Default != "" {
					sb.WriteString(fmt.Sprintf(", default: %s", exp.Default))
				}
				if exp.Stable != "" {
					sb.WriteString(fmt.Sprintf(", stable: %s", exp.Stable))
				}
				if exp.Withdrawn != "" {
					sb.WriteString(fmt.Sprintf(", withdrawn: %s", exp.Withdrawn))
				}
				sb.WriteString(")")
			} else if exp.Withdrawn != "" {
				sb.WriteString(" (withdrawn)")
			}
			sb.WriteString("\n")

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
	}

	sb.WriteString(`Experiment lifecycle:
- preview: experimental feature that can be enabled
- default: feature enabled by default, can still be disabled
- stable: feature permanently enabled, experiment flag has no effect
- withdrawn: experiment removed, flag has no effect

Language experiments may change behavior, syntax, or semantics.
Use with caution in production code.
`)
	sb.WriteString("`")
	sb.WriteString("[1:],\n}\n")

	return sb.String()
}

// validateURLsInComments checks that all URLs found in experiment comments are valid
func validateURLsInComments(experiments []Experiment) {
	validURLPattern := regexp.MustCompile(`^https://cuelang\.org/(issue|cl|discussion)/\d+$`)

	for _, exp := range experiments {
		if exp.Comment == "" {
			continue
		}

		// Find all URLs in the comment
		urlPattern := regexp.MustCompile(`https://[^\s]+`)
		urls := urlPattern.FindAllString(exp.Comment, -1)

		for _, url := range urls {
			// Remove trailing punctuation for validation
			cleanURL := strings.TrimRight(url, ".")
			if !validURLPattern.MatchString(cleanURL) {
				log.Printf("WARNING: Invalid URL format in %s experiment: %s", exp.Name, url)
			}
		}
	}
}
