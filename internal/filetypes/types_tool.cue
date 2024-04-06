package build

import (
	"encoding/json"
	"tool/file"
)

out: {
	knownExtensions: {
		for ext, _ in extensions if ext != "" {
			(ext): true
		}
	}
	simpleModeFiles: {
		for mode, modeinfo in modes {
			(mode): {
				default: #Default & modeinfo.#Default
				byExtension: {for k, v in extensions & modeinfo.extensions if k != "" {(k): v}}
			}
		}
	}
}

command: gen: {
	print: file.Create & {
		filename: "types_gen.json"
		contents: json.Indent(json.Marshal(out), "", "\t") + "\n"
	}
}
