package home

// deliberately put in another file to test resolving top-level identifiers
// in different files.
runBase: {
	task echo: {
		kind:   "exec"
		stdout: string
	}

	task display: {
		kind: "print"
		text: task.echo.stdout
	}
}
