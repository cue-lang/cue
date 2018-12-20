package home

command run: runBase & {
	task echo cmd: "echo \(message)"
}

command run_list: runBase & {
	task echo cmd: ["echo", message]
}

// TODO: capture stdout and stderr for tests.
command runRedirect: {
	task echo: {
		kind:   "exec"
		cmd:    "echo \(message)"
		stdout: null // should be automatic
	}
}

command baddisplay: {
	task display: {
		kind: "print"
		text: 42
	}
}

command http: {
	task testserver: {
		kind: "testserver"
		url:  string
	}
	task http: {
		kind:   "http"
		method: "POST"
		url:    task.testserver.url

		request body:  "I'll be back!"
		response body: string // TODO: allow this to be a struct, parsing the body.
	}
	task print: {
		kind: "print"
		text: task.http.response.body
	}
}
