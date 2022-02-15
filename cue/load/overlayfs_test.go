package load

import (
	"bytes"
	"io"
	"testing"
)

func TestOverlayFile_Read(t *testing.T) {
	fileContents := []byte("Hello World")

	file := &overlayFile{
		contents: fileContents,
	}

	var buf bytes.Buffer
	if _, err := io.Copy(&buf, file); err != nil {
		t.Errorf("An error occured while reading the overlay file")
	}

	if string(fileContents) != string(buf.Bytes()) {
		t.Errorf("The overlay file was read incorrectly")
	}
}
