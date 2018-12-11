package hello

command echo: {
    task echo: {
        kind:   "exec"
        cmd:    "echo \(message)"
        stdout: string
    }

    task display: {
        kind: "print"
        text: task.echo.stdout
    }
}