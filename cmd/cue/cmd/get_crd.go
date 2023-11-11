// Copyright 2018 The CUE Authors
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

package cmd

import (
	"cuelang.org/go/cue/build"
	"cuelang.org/go/cue/load"
	"github.com/spf13/cobra"
)

func newCrdCmd(c *Command) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "crd [files]",
		Short: "add Kubernetes CustomResourceDefinition dependencies to the current module",
		Long: `crd converts Kubernetes resources defined by a CustomResourceDefinition into CUE definitions

The command "cue get crd" converts the Kubernetes CustomResourceDefinition
to CUE. The retrieved definitions are put in the CUE module's pkg
directory at the API group name of the corresponding resource. The converted
definitions are available to any CUE file within the CUE module by using
this name.

The CustomResourceDefinition is converted to CUE based on how it would be
interpreted by the Kubernetes API server. Definitions for a CRD with group
name "myresource.example.com" and version "v1" will be written to a CUE 
file named myresource.example.com/v1/types_crd_gen.cue.

It is safe for users to add additional files to the generated directories,
as long as their name does not end with _gen.*.


Rules of Converting CustomResourceDefinitions to CUE

CustomResourceDefinitions are converted to cue structs adhering to the following conventions:

	- OpenAPIv3 schema is imported the same as "cue import openapi".

	- The @x-kubernetes-validation attribute is added if the field utilizes the "x-kubernetes-validation" extension.
`,

		RunE: mkRunE(c, extract),
	}

	return cmd
}

func runGetCRD(cmd *Command, args []string) error {
	c := &config{
		fileFilter:     `\.(json|yaml|yml|jsonl|ldjson)$`,
		interpretation: build.CustomResourceDefinition,
		encoding:       "yaml",
		loadCfg:        &load.Config{DataFiles: true},
	}

	p, err := newBuildPlan(cmd, c)
	if err != nil {
		return err
	}

	return nil
}
