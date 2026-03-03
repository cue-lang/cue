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
	"slices"
	"strings"
	"sync"

	"cuelang.org/go/internal/golangorgx/gopls/protocol"
	"cuelang.org/go/internal/lsp/fscache"
	"cuelang.org/go/unstable/lspaux/config"
	extproto "cuelang.org/go/unstable/lspaux/protocol"
)

type ExtValidatorManager struct {
	profile    *config.Profile
	fs         *fscache.OverlayFS
	debugLog   func(msg string)
	validators map[protocol.DocumentURI]*ExtValidator
}

func NewExtValidatorManager(profile *config.Profile, fs *fscache.OverlayFS, debugLog func(msg string)) *ExtValidatorManager {
	return &ExtValidatorManager{
		profile:    profile,
		fs:         fs,
		debugLog:   debugLog,
		validators: make(map[protocol.DocumentURI]*ExtValidator),
	}
}

// EnsureExtValidator tests to see if the fileUri should be linked to
// an external validator [ExtValidator]. Currently, it tests to see if
// there is a ".git" directory in the file's directory or any parent
// directory. If no such directory is found, nil will be returned.
//
// An ExtValidator can be clean or dirty. It can be marked dirty
// either by the external validator informing the LSP, or by the LSP
// choosing to mark the external validator as dirty. When an
// evaluation is requested, the external validator is marked clean. In
// all cases, as the external validator transitions from clean to
// dirty, or dirty to clean, the onDirtyChange callback will be
// invoked, if supplied.
func (mgr *ExtValidatorManager) EnsureExtValidator(fileUri protocol.DocumentURI, onDirtyChange func(*ExtValidator)) *ExtValidator {
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
	if !found {
		v = &ExtValidator{
			mgr:           mgr,
			rootURI:       fileUri,
			onDirtyChange: onDirtyChange,
		}
		mgr.debugLog(fmt.Sprintf("extValidators: creating external validator for %v", fileUri))
		v.conn = connect(mgr.profile, nil, v, mgr.debugLog)
		mgr.validators[fileUri] = v
		v.MarkDirty()
	}
	return v
}

func isDir(fs iofs.StatFS, uri protocol.DocumentURI) bool {
	path := strings.TrimLeft(uri.Path(), "/")
	info, err := fs.Stat(path)
	return err == nil && info.IsDir()
}

// ExtValidator models an external validator and provides the ability
// to request evaluations.
type ExtValidator struct {
	mgr           *ExtValidatorManager
	rootURI       protocol.DocumentURI
	conn          *conn
	onDirtyChange func(*ExtValidator)

	lock          sync.Mutex
	evalRequestId int
	eval          *evaluation
	isDirty       bool
}

func (v *ExtValidator) IsDirty() bool {
	v.lock.Lock()
	defer v.lock.Unlock()
	return v.isDirty
}

type EvalResponseHandler interface {
	// Called for each evaluation result received from the server. NB
	// this may never be called if a subsequent new evaluation request
	// is made before any results are received.
	EvalResult(*extproto.EvalResultMsg)
	// Called once all evaluation results have been received by the
	// server. NB this may never be called if a new evaluation request
	// is made before the server indicates it has sent all the results.
	EvalFinished(*extproto.EvalFinishedMsg)
	// Called when this evaluation request is replaced with a new
	// evaluation request. This will always be called, regardless of
	// how many results have been received, at the point that a new
	// evaluation request is made.
	Clear()
}

type evaluation struct {
	handler       EvalResponseHandler
	versionedURIs map[protocol.DocumentURI]int32
	requestId     string
}

// RequestEvaluation creates and sends an evaluation request to the
// external validator. If an evaluation already exists, its handler's
// [EvalResponseHandler.Clear] method is invoked.
func (v *ExtValidator) RequestEvaluation(handler EvalResponseHandler) error {
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

	v.lock.Lock()
	v.evalRequestId += 1
	requestId := fmt.Sprint(v.evalRequestId)

	oldEval := v.eval
	v.eval = &evaluation{
		handler:       handler,
		versionedURIs: versionedURIs,
		requestId:     requestId,
	}
	v.lock.Unlock()

	if oldEval != nil {
		go oldEval.handler.Clear()
	}

	msg := &extproto.EvalRequestMsg{
		RequestID: requestId,
		RepoName:  repoName,
		CommitID:  commitId,
		ZipData:   buf.Bytes(),
	}
	v.conn.debugLogf("extValidator: sending evaluation request; id: %s; repo: %s; commit: %s", requestId, repoName, commitId)
	if v.conn.requestEvaluation(msg) {
		v.setDirty(false)
	}

	return nil
}

func (v *ExtValidator) commitId() (string, error) {
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
func (v *ExtValidator) repoName() (string, error) {
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
func (v *ExtValidator) trackedFiles() ([]string, error) {
	data, err := v.runGit("ls-files", "--full-name", "-z", ":/")
	if err != nil {
		return nil, err
	}
	return strings.Split(string(data), "\000"), nil
}

func (v *ExtValidator) runGit(args ...string) ([]byte, error) {
	dir := v.rootURI.FilePath()
	args = append([]string{"--git-dir=" + dir + "/.git", "--work-tree=" + dir}, args...)
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = []string{"GIT_CONFIG_NOSYSTEM=1"} // also nuke out all existing env
	return cmd.Output()
}

// MarkDirty ensures the [ExtValidator] is considered dirty. If it was
// previously clean, the onDirtyChange callback will be invoked, if it
// was supplied to [EnsureExtValidator].
func (v *ExtValidator) MarkDirty() {
	v.setDirty(true)
}

func (v *ExtValidator) setDirty(isDirty bool) {
	v.lock.Lock()
	var onDirtyChange func(*ExtValidator)
	if v.isDirty != isDirty {
		v.isDirty = isDirty
		onDirtyChange = v.onDirtyChange
	}
	v.lock.Unlock()

	if onDirtyChange != nil {
		onDirtyChange(v)
	}
}

// connected implements [extValidatorClient]
func (v *ExtValidator) connected() {
	v.MarkDirty()
}

// changeSignal implements [extValidatorClient]
func (v *ExtValidator) changeSignal(*extproto.ChangedMsg) error {
	v.conn.debugLog("extValidator: received change signal")
	v.MarkDirty()
	return nil
}

// evalResult implements [extValidatorClient]
func (v *ExtValidator) evalResult(msg *extproto.EvalResultMsg) error {
	v.lock.Lock()
	eval := v.eval
	v.lock.Unlock()

	if eval == nil || msg.RequestID != eval.requestId {
		return nil
	}

	v.conn.debugLogf("extValidator: recevied evaluation result; id %s", eval.requestId)

	versionedURIs := eval.versionedURIs

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
	eval.handler.EvalResult(msg)

	return nil
}

// evalFinished implements [extValidatorClient]
func (v *ExtValidator) evalFinished(msg *extproto.EvalFinishedMsg) error {
	v.lock.Lock()
	eval := v.eval
	v.lock.Unlock()

	if eval == nil || msg.RequestID != eval.requestId {
		return nil
	}

	v.conn.debugLogf("extValidator: recevied evaluation finished; id %s", eval.requestId)

	eval.handler.EvalFinished(msg)

	return nil
}

// addFS adds the trackedFiles (and their directories) to the supplied
// [zip.Writer]. trackedFiles must be /-separated paths, relative to
// rootURI.
func addFS(w *zip.Writer, fs *fscache.OverlayFS, rootURI protocol.DocumentURI, trackedFiles []string) (map[protocol.DocumentURI]int32, error) {
	rootPath := rootURI.Path() + "/"
	rootFilePath := rootURI.FilePath()

	versionedURIs := make(map[protocol.DocumentURI]int32)
	ensureDirectory := func(uri protocol.DocumentURI) error {
		var dirs []string
		for ; rootURI.Encloses(uri); uri = uri.Dir() {
			if _, found := versionedURIs[uri]; found {
				break
			}
			versionedURIs[uri] = 0
			dirs = append(dirs, uri.Path()+"/")
		}

		for _, dir := range slices.Backward(dirs) {
			relativePath := strings.TrimPrefix(dir, rootPath)
			if relativePath == "" {
				continue
			}
			h := &zip.FileHeader{
				Name:   relativePath + "/",
				Method: zip.Deflate,
			}
			_, err := w.CreateHeader(h)
			if err != nil {
				return err
			}

		}

		return nil
	}

	for _, name := range trackedFiles {
		uri := protocol.URIFromPath(filepath.Join(rootFilePath, filepath.FromSlash(name)))
		if _, found := versionedURIs[uri]; found {
			continue
		}
		err := ensureDirectory(uri.Dir())
		if err != nil {
			return nil, err
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
