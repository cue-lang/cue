// Code generated by cue get go. DO NOT EDIT.

// Package file provides file operations for cue tasks.
//
// These are the supported tasks:
//
//     // Read reads the contents of a file.
//     Read: {
//     	$id: "tool/file.Read"
//
//     	// filename names the file to read.
//     	//
//     	// Relative names are taken relative to the current working directory.
//     	// Slashes are converted to the native OS path separator.
//     	filename: !=""
//
//     	// contents is the read contents. If the contents are constraint to bytes
//     	// (the default), the file is read as is. If it is constraint to a string,
//     	// the contents are checked to be valid UTF-8.
//     	contents: *bytes | string
//     }
//
//     // Append writes contents to the given file.
//     Append: {
//     	$id: "tool/file.Append"
//
//     	// filename names the file to append.
//     	//
//     	// Relative names are taken relative to the current working directory.
//     	// Slashes are converted to the native OS path separator.
//     	filename: !=""
//
//     	// permissions defines the permissions to use if the file does not yet exist.
//     	permissions: int | *0o666
//
//     	// contents specifies the bytes to be written.
//     	contents: bytes | string
//     }
//
//     // Create writes contents to the given file.
//     Create: {
//     	$id: "tool/file.Create"
//
//     	// filename names the file to write.
//     	//
//     	// Relative names are taken relative to the current working directory.
//     	// Slashes are converted to the native OS path separator.
//     	filename: !=""
//
//     	// permissions defines the permissions to use if the file does not yet exist.
//     	permissions: int | *0o666
//
//     	// contents specifies the bytes to be written.
//     	contents: bytes | string
//     }
//
//     // Glob returns a list of files.
//     Glob: {
//     	$id: "tool/file.Glob"
//
//     	// glob specifies the pattern to match files with.
//     	//
//     	// A relative pattern is taken relative to the current working directory.
//     	// Slashes are converted to the native OS path separator.
//     	glob: !=""
//     	files: [...string]
//     }
//
//     // Mkdir creates a directory at the specified path.
//     Mkdir: {
//     	$id: "tool/file.Mkdir"
//
//     	// The directory path to create.
//     	// If path is already a directory, Mkdir does nothing.
//     	// If path already exists and is not a directory, Mkdir will return an error.
//     	path: string
//
//     	// When true any necessary parents are created as well.
//     	createParents: bool | *false
//
//     	// Directory mode and permission bits (before umask).
//     	permissions: int | *0o755
//     }
//
//     // MkdirAll creates a directory at the specified path along with any necessary
//     // parents.
//     // If path is already a directory, MkdirAll does nothing.
//     // If path already exists and is not a directory, MkdirAll will return an error.
//     MkdirAll: Mkdir & {
//     	createParents: true
//     }
//
package file
