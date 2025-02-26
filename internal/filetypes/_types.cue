// Copyright 2020 CUE Authors
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

package build

#File: {
	filename?:       string
	encoding!:       #Encoding
	interpretation?: #Interpretation
	form?:           #Form
}

// Must be embedding
#FileInfo: {#File}

fileForExtVanilla: modes.input.extensions

modes: input: {
	Default: {}
	FileInfo: #FileInfo
	extensions: ".json": interpretation: "auto"
}

#Encoding:       !="" // | error("no encoding specified")
#Interpretation: string
#Form:           string

modes: [string]: extensions: {
	".cue":  tagInfo.cue
	".json": tagInfo.json
}

forms: [Name=string]: #FileInfo
forms: schema: {}

interpretations: auto: forms.schema

tagInfo: {
	cue: encoding:  "cue"
	json: encoding: "json"
}
