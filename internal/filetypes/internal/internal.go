// Package internal holds some internal parts of the filetypes package
// that need to be shared between the code generator and the package proper.
package internal

type ErrorKind int

const (
	ErrNoError ErrorKind = iota
	ErrUnknownFileExtension
	ErrCouldNotDetermineFileType
	ErrNoEncodingSpecified
	NumErrorKinds
)
