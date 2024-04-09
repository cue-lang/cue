package vcs

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

type VCS interface {
	// Root returns the root of the VCS directory.
	Root() string

	// ListFiles returns a list of all the files tracked by the VCS under the given
	// directory, relative to that directory, as filepaths, in lexical order.
	// It does not include directory names.
	//
	// The directory should be within the VCS root.
	ListFiles(ctx context.Context, dir string) ([]string, error)

	// Status returns the status of the repository holding the
	// given directory.
	Status(ctx context.Context) (Status, error)
}

// Status is the current state of a local repository.
type Status struct {
	Revision    string    // Optional.
	CommitTime  time.Time // Optional.
	Uncommitted bool      // Required.
}

var vcsTypes = map[string]func(dir string) (VCS, error){
	"git": newGitVCS,
}

func New(vcsType string, dir string) (VCS, error) {
	vf := vcsTypes[vcsType]
	if vf == nil {
		return nil, fmt.Errorf("unrecognized VCS type %q", vcsType)
	}
	return vf(dir)
}

// findRoot inspects dir and its parents to find
// the VCS repository signified the presence of
// one of the given root names.
//
// If no repository is found, findRoot returns
// the empty string.
func findRoot(dir string, rootNames []string) string {
	dir = filepath.Clean(dir)
	for {
		if isVCSRoot(dir, rootNames) {
			return dir
		}
		ndir := filepath.Dir(dir)
		if len(ndir) >= len(dir) {
			break
		}
		dir = ndir
	}
	return ""
}

// isVCSRoot identifies a VCS root by checking whether the directory contains
// any of the listed root names.
func isVCSRoot(dir string, rootNames []string) bool {
	for _, root := range rootNames {
		if _, err := os.Stat(filepath.Join(dir, root)); err == nil {
			// TODO return false if it's not the expected file type.
			// For now, this is only used by git which can use both
			// files and directories, so we'll allow either.
			return true
		}
	}
	return false
}

func runCmd(ctx context.Context, dir string, cmdName string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, cmdName, args...)
	cmd.Dir = dir

	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("running %q %q: %v", cmdName, args, err)
	}
	return string(out), nil
}

type vcsNotFoundError struct {
	kind string
	dir  string
}

func (e *vcsNotFoundError) Error() string {
	return fmt.Sprintf("%s VCS not found in any parent of %q", e.kind, e.dir)
}
