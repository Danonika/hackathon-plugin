package mcp

import (
	"io"
	"os"
)

func stdinReader() io.Reader {
	return os.Stdin
}

func stdoutWriter() io.Writer {
	return os.Stdout
}
