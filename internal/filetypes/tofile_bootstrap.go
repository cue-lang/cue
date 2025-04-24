//go:build bootstrap

package filetypes

import "cuelang.org/go/cue/build"

func toFileGenerated(mode Mode, sc *scope, filename string) (*build.File, error) {
	panic("never called")
}

func fromFileGenerated(b *build.File, mode Mode) (*FileInfo, error) {
	panic("never called")
}
