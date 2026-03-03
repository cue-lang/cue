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

package cuehub

import (
	"archive/zip"
	"bytes"
	"encoding/hex"
	"fmt"
	"io"
	iofs "io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"cuelang.org/go/internal/golangorgx/gopls/protocol"
	"cuelang.org/go/internal/lsp/fscache"
	cuehubproto "cuelang.org/go/unstable/lspaux/protocol"
)

type CueHubManager struct {
	serverUrl string
	fs        *fscache.OverlayFS
	debugLog  func(msg string)
	hubs      map[protocol.DocumentURI]*CueHub
}

func NewCueHubManager(serverUrl string, fs *fscache.OverlayFS, debugLog func(msg string)) *CueHubManager {
	return &CueHubManager{
		serverUrl: serverUrl,
		fs:        fs,
		debugLog:  debugLog,
		hubs:      make(map[protocol.DocumentURI]*CueHub),
	}
}

func (mgr *CueHubManager) EnsureHub(uri protocol.DocumentURI) *CueHub {
	fs := mgr.fs.IoFS(string(os.PathSeparator))
	found := false
	var oldUri protocol.DocumentURI
	for ; uri != oldUri; oldUri, uri = uri, uri.Dir() {
		if isDir(fs, uri+"/.cuehub") && isDir(fs, uri+"/.git") {
			found = true
			break
		}
	}
	if !found {
		return nil
	}

	hub, found := mgr.hubs[uri]
	if !found {
		hub = &CueHub{
			mgr:     mgr,
			rootURI: uri,
			isDirty: true,
		}
		mgr.debugLog(fmt.Sprintf("cuehub: creating hub at %v", uri))
		hub.conn = connect(mgr.serverUrl, nil, (*hubClient)(hub), mgr.debugLog)
		mgr.hubs[uri] = hub
	}
	return hub
}

func isDir(fs iofs.StatFS, uri protocol.DocumentURI) bool {
	path := strings.TrimLeft(uri.Path(), "/")
	info, err := fs.Stat(path)
	return err == nil && info.IsDir()
}

type CueHub struct {
	mgr     *CueHubManager
	rootURI protocol.DocumentURI
	conn    *cueHubConnection

	lock          sync.Mutex
	evalRequestId int
	eval          *Evaluation
	isDirty       bool
}

func (hub *CueHub) IsDirty() bool {
	hub.lock.Lock()
	defer hub.lock.Unlock()
	return hub.isDirty
}

type EvalResponseHandler interface {
	EvalResult(*Evaluation, *cuehubproto.EvalResultMsg)
	EvalFinished(*Evaluation, *cuehubproto.EvalFinishedMsg)
	Clear(*Evaluation)
}

type Evaluation struct {
	Handler       EvalResponseHandler
	VersionedURIs map[protocol.DocumentURI]int32
	requestId     string
}

func (hub *CueHub) RequestEvaluation(handler EvalResponseHandler) (*Evaluation, error) {
	repoName, err := hub.repoName()
	if err != nil {
		return nil, err
	}

	commitId, err := hub.commitId()
	if err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	versionedURIs, err := addFS(w, hub.mgr.fs, hub.rootURI)
	if err != nil {
		return nil, err
	}
	err = w.Close()
	if err != nil {
		return nil, err
	}

	eval := &Evaluation{
		Handler:       handler,
		VersionedURIs: versionedURIs,
	}

	hub.lock.Lock()
	hub.isDirty = false

	hub.evalRequestId += 1
	requestId := fmt.Sprint(hub.evalRequestId)

	oldEval := hub.eval
	eval.requestId = requestId
	hub.eval = eval
	hub.lock.Unlock()

	if oldEval != nil {
		go oldEval.Handler.Clear(oldEval)
	}

	msg := &cuehubproto.EvalRequestMsg{
		RequestID: requestId,
		RepoName:  "file:demo/lsp/source", //repoName,
		CommitID:  commitId,
		ZipData:   buf.Bytes(),
	}
	hub.conn.debugLogf("cuehub: sending evaluation request; id: %s; repo: %s; commit: %s", requestId, repoName, commitId)
	hub.conn.requestEvaluation(msg)

	return eval, nil
}

func (hub *CueHub) commitId() (string, error) {
	data, err := hub.runGit("rev-parse", "--verify", "HEAD")
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

func (hub *CueHub) repoName() (string, error) {
	data, err := hub.runGit("config", "--local", "remote.origin.url")
	if err != nil {
		return "", err
	}
	data = bytes.TrimSpace(data)
	return string(data), nil
}

func (hub *CueHub) runGit(args ...string) ([]byte, error) {
	dir := hub.rootURI.FilePath()
	args = append([]string{"--git-dir=" + dir + "/.git", "--work-tree=" + dir}, args...)
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = []string{"GIT_CONFIG_NOSYSTEM=1"} // also nuke out all existing env
	return cmd.Output()
}

func (hub *CueHub) MarkDirty() {
	hub.lock.Lock()
	hub.isDirty = true
	hub.lock.Unlock()
}

type hubClient = CueHub

func (hub *hubClient) ChangeSignal(*cuehubproto.ChangedMsg) error {
	hub.conn.debugLog("cuehub: received change signal")
	hub.MarkDirty()
	return nil
}

func (hub *hubClient) EvalResult(msg *cuehubproto.EvalResultMsg) error {
	hub.lock.Lock()
	eval := hub.eval
	hub.lock.Unlock()

	if eval == nil || msg.RequestID != eval.requestId {
		return nil
	}

	hub.conn.debugLogf("cuehub: recevied evaluation result; id %s", eval.requestId)

	versionedURIs := eval.VersionedURIs

	rootURI := hub.rootURI + "/"
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
	eval.Handler.EvalResult(eval, msg)

	return nil
}

func (hub *hubClient) EvalFinished(msg *cuehubproto.EvalFinishedMsg) error {
	hub.lock.Lock()
	eval := hub.eval
	hub.lock.Unlock()

	if eval == nil || msg.RequestID != eval.requestId {
		return nil
	}

	hub.conn.debugLogf("cuehub: recevied evaluation finished; id %s", eval.requestId)

	eval.Handler.EvalFinished(eval, msg)

	return nil
}

func addFS(w *zip.Writer, fs *fscache.OverlayFS, rootURI protocol.DocumentURI) (map[protocol.DocumentURI]int32, error) {
	rootPath := rootURI.Path() + "/"

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

	err := fs.View(func(txn *fscache.ViewTxn) error {
		txn.WalkFiles(func(fh fscache.FileHandle) error {
			uri := fh.URI()
			err := ensureDirectory(uri.Dir())
			if err != nil {
				return err
			}
			versionedURIs[uri] = fh.Version()

			h, err := zip.FileInfoHeader(fh.(iofs.FileInfo))
			if err != nil {
				return err
			}
			h.Name = strings.TrimPrefix(uri.Path(), rootPath)
			h.Method = zip.Deflate

			fw, err := w.CreateHeader(h)
			if err != nil {
				return err
			}
			_, err = fw.Write(fh.Content())
			return err
		}, rootURI)

		return nil
	})
	if err != nil {
		return nil, err
	}

	fsys := fs.IoFS(rootURI.FilePath())
	err = iofs.WalkDir(fsys, ".", func(name string, d iofs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if name == "." {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if !d.IsDir() && !info.Mode().IsRegular() {
			return nil
		}

		if leaf := path.Base(name); strings.HasPrefix(leaf, ".") {
			if d.IsDir() {
				return iofs.SkipDir
			} else {
				return nil
			}
		}

		uri := protocol.URIFromPath(filepath.FromSlash(rootPath + name))
		if _, found := versionedURIs[uri]; found {
			return nil
		}
		versionedURIs[uri] = 0

		h, err := zip.FileInfoHeader(info)
		if err != nil {
			return err
		}
		h.Name = name
		if d.IsDir() {
			h.Name += "/"
		}
		h.Method = zip.Deflate
		fw, err := w.CreateHeader(h)
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		f, err := fsys.Open(name)
		if err != nil {
			return err
		}
		defer f.Close()
		_, err = io.Copy(fw, f)
		return err
	})

	if err != nil {
		return nil, err
	}

	return versionedURIs, nil
}
