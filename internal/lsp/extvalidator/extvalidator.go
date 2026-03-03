// Copyright 2026 CUE Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// extvalidator supports connecting to, and communicating with,
// external validation servers.
package extvalidator

import (
	"archive/zip"
	"bytes"
	"encoding/hex"
	"fmt"
	iofs "io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"cuelang.org/go/internal/golangorgx/gopls/protocol"
	"cuelang.org/go/internal/lsp/fscache"
	extproto "cuelang.org/go/unstable/lspaux/protocol"
	"cuelang.org/go/unstable/lspaux/validatorconfig"
)

type Manager struct {
	profile    *validatorconfig.Profile
	fs         *fscache.OverlayFS
	debugLog   func(msg string)
	validators map[protocol.DocumentURI]*Validator
}

// NewManager creates a new [Manager] which can be used to manage
// connections between repos within fs, and the external validator
// server indicated by profile.
func NewManager(profile *validatorconfig.Profile, fs *fscache.OverlayFS, debugLog func(msg string)) *Manager {
	return &Manager{
		profile:    profile,
		fs:         fs,
		debugLog:   debugLog,
		validators: make(map[protocol.DocumentURI]*Validator),
	}
}

// EnsureValidator creates (if necessary) and returns an external
// validator, appropriate for the given fileUri.
//
// Currently, it tests to see if there is a ".git" directory in the
// file's directory or any parent directory. If no such directory is
// found, nil will be returned.
func (mgr *Manager) EnsureValidator(fileUri protocol.DocumentURI, onDirtyChange func(*Validator)) *Validator {
	fs := mgr.fs.IoFS(string(os.PathSeparator))
	found := false
	var oldUri protocol.DocumentURI
	for ; fileUri != oldUri; oldUri, fileUri = fileUri, fileUri.Dir() {
		if isDir(fs, fileUri+"/.git") {
			found = true
			break
		}
	}
	if !found {
		return nil
	}

	v, found := mgr.validators[fileUri]
	if found {
		return v
	}

	v = &Validator{
		mgr:           mgr,
		rootURI:       fileUri,
		onDirtyChange: onDirtyChange,
	}
	mgr.debugLog(fmt.Sprintf("extValidators: creating external validator for %v", fileUri))
	v.conn = connect(mgr.profile, nil, v, mgr.debugLog)
	mgr.validators[fileUri] = v
	v.MarkDirty()
	return v
}

func isDir(fs iofs.StatFS, uri protocol.DocumentURI) bool {
	path := strings.TrimLeft(uri.Path(), "/")
	info, err := fs.Stat(path)
	return err == nil && info.IsDir()
}

// Validator represents an external validator for an entire
// repository.
//
// A Validator can be clean or dirty. It can be marked dirty either by
// the external validator informing the LSP, or by the LSP choosing to
// mark the external validator as dirty. When a validation is started,
// the validator is marked clean. In all cases, as the validator
// transitions from clean to dirty, or dirty to clean, the
// onDirtyChange callback will be invoked, if supplied.
type Validator struct {
	mgr           *Manager
	rootURI       protocol.DocumentURI
	conn          *conn
	onDirtyChange func(*Validator)

	mu        sync.Mutex
	requestId int
	cur       *validation
	isDirty   bool
}

// IsDirty reports if the validator is currently dirty.
func (v *Validator) IsDirty() bool {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.isDirty
}

type ResponseHandler interface {
	// Result is called for each validation result received from the
	// server. NB this may never be called if a subsequent new
	// validation request is made before any results are received.
	Result(*extproto.EvalResultMsg)
	// Finished is called once all validation results have been
	// received by the server. NB this may never be called if a new
	// validation request is made before the server indicates it has
	// sent all the results.
	Finished(*extproto.EvalFinishedMsg)
	// Clear is called when this validation request is replaced with a
	// new validation request. This will always be called, regardless
	// of how many results have been received, at the point that a new
	// validation request is made.
	Clear()
}

type validation struct {
	handler       ResponseHandler
	versionedURIs map[protocol.DocumentURI]int32
	requestId     string
}

// StartValidation creates and sends a validation request to the
// external validator. If a validation already exists, its handler's
// [ResponseHandler.Clear] method is invoked.
func (v *Validator) StartValidation(handler ResponseHandler) error {
	repoName, err := v.repoName()
	if err != nil {
		return err
	}

	commitId, err := v.commitId()
	if err != nil {
		return err
	}

	trackedFiles, err := v.trackedFiles()
	if err != nil {
		return err
	}

	// TODO: switch to sending diffs.
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	versionedURIs, err := addFS(w, v.mgr.fs, v.rootURI, trackedFiles)
	if err != nil {
		return err
	}
	err = w.Close()
	if err != nil {
		return err
	}

	v.mu.Lock()
	v.requestId++
	requestId := fmt.Sprint(v.requestId)

	oldValidation := v.cur
	v.cur = &validation{
		handler:       handler,
		versionedURIs: versionedURIs,
		requestId:     requestId,
	}
	v.mu.Unlock()

	if oldValidation != nil {
		go oldValidation.handler.Clear()
	}

	msg := &extproto.EvalRequestMsg{
		RequestID: requestId,
		RepoName:  repoName,
		CommitID:  commitId,
		ZipData:   buf.Bytes(),
	}
	v.conn.debugLogf("extValidator: sending validation request; id: %s; repo: %s; commit: %s", requestId, repoName, commitId)
	if v.conn.requestEvaluation(msg) {
		v.setDirty(false)
	}

	return nil
}

func (v *Validator) commitId() (string, error) {
	data, err := v.runGit("rev-parse", "--verify", "HEAD")
	if err != nil {
		return "", err
	}
	data = bytes.TrimSpace(data)
	out := make([]byte, hex.DecodedLen(len(data)))
	_, err = hex.Decode(out, data)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// repoName returns the url of the "origin" remote from the git repo.
//
// In reality there is no single reponame, and this current approach
// is only likely to work for a subset of use-cases. TODO: find a
// better solution for the naming of sources.
func (v *Validator) repoName() (string, error) {
	data, err := v.runGit("config", "--local", "remote.origin.url")
	if err != nil {
		return "", err
	}
	url := string(bytes.TrimSpace(data))
	if withoutGitHub, wasCut := strings.CutPrefix(url, "git@github.com:"); wasCut {
		// transform "git@github.com:foo/bar.git" into "github:foo/bar"
		url = "github:" + strings.TrimSuffix(withoutGitHub, ".git")
	}
	return url, nil
}

// trackedFiles returns all the tracked files within the validator's
// git-repo. All the paths returned are /-separated and relative to
// the repo's root.
func (v *Validator) trackedFiles() ([]string, error) {
	data, err := v.runGit("ls-files", "--full-name", "-z", ":/")
	if err != nil {
		return nil, err
	}
	data = bytes.Trim(data, "\000")
	return strings.Split(string(data), "\000"), nil
}

func (v *Validator) runGit(args ...string) ([]byte, error) {
	dir := v.rootURI.FilePath()
	args = append([]string{"--git-dir=" + dir + "/.git", "--work-tree=" + dir}, args...)
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = []string{"GIT_CONFIG_NOSYSTEM=1"} // also nuke out all existing env
	return cmd.Output()
}

// MarkDirty ensures the [Validator] is considered dirty. If it was
// previously clean, the onDirtyChange callback will be invoked, if it
// was supplied to [EnsureExtValidator].
func (v *Validator) MarkDirty() {
	v.setDirty(true)
}

func (v *Validator) setDirty(isDirty bool) {
	v.mu.Lock()
	var onDirtyChange func(*Validator)
	if v.isDirty != isDirty {
		v.isDirty = isDirty
		onDirtyChange = v.onDirtyChange
	}
	v.mu.Unlock()

	if onDirtyChange != nil {
		onDirtyChange(v)
	}
}

// connected implements [extValidatorClient]
func (v *Validator) connected() {
	v.MarkDirty()
}

// changeSignal implements [extValidatorClient]
func (v *Validator) changeSignal(*extproto.ChangedMsg) error {
	v.conn.debugLog("extValidator: received change signal")
	v.MarkDirty()
	return nil
}

// evalResult implements [extValidatorClient]
func (v *Validator) evalResult(msg *extproto.EvalResultMsg) error {
	v.mu.Lock()
	validation := v.cur
	v.mu.Unlock()

	if validation == nil || msg.RequestID != validation.requestId {
		return nil
	}

	v.conn.debugLogf("extValidator: recevied validation result; id %s", validation.requestId)

	versionedURIs := validation.versionedURIs

	rootURI := v.rootURI + "/"
	for i := range msg.Errors {
		err := &msg.Errors[i]
		coords := err.Coordinates[:0]
		for _, coord := range err.Coordinates {
			// TODO: coord.Path really needs to turn into a proper
			// URI. Currently it could have raw spaces in it etc which
			// would be problematic.
			uri := rootURI + protocol.DocumentURI(coord.Path)
			if _, found := versionedURIs[uri]; !found {
				continue
			}
			coord.Path = string(uri)
			coords = append(coords, coord)
		}
		err.Coordinates = coords
	}
	validation.handler.Result(msg)

	return nil
}

// evalFinished implements [extValidatorClient]
func (v *Validator) evalFinished(msg *extproto.EvalFinishedMsg) error {
	v.mu.Lock()
	validation := v.cur
	v.mu.Unlock()

	if validation == nil || msg.RequestID != validation.requestId {
		return nil
	}

	v.conn.debugLogf("extValidator: validation finished; id %s", validation.requestId)

	validation.handler.Finished(msg)

	return nil
}

// addFS adds the trackedFiles (and their directories) to the supplied
// [zip.Writer]. trackedFiles must be /-separated paths, relative to
// rootURI.
func addFS(w *zip.Writer, fs *fscache.OverlayFS, rootURI protocol.DocumentURI, trackedFiles []string) (map[protocol.DocumentURI]int32, error) {
	rootFilePath := rootURI.FilePath()

	versionedURIs := make(map[protocol.DocumentURI]int32)

	for _, name := range trackedFiles {
		uri := protocol.URIFromPath(filepath.Join(rootFilePath, filepath.FromSlash(name)))
		if _, found := versionedURIs[uri]; found {
			continue
		}

		fh, err := fs.ReadFile(uri)
		if err != nil {
			return nil, err
		}
		versionedURIs[uri] = fh.Version()
		h := &zip.FileHeader{
			Name:               name,
			UncompressedSize64: uint64(len(fh.Content())),
			Method:             zip.Deflate,
			Modified:           fh.ModTime().UTC(),
		}

		fw, err := w.CreateHeader(h)
		if err != nil {
			return nil, err
		}
		_, err = fw.Write(fh.Content())
		if err != nil {
			return nil, err
		}
	}

	return versionedURIs, nil
}
