package cache

import (
	"archive/zip"
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"strings"
	"sync"
	"time"

	"cuelang.org/go/cue/token"
	"cuelang.org/go/internal/golangorgx/gopls/protocol"
	"github.com/coder/websocket"
	"github.com/msackman/riskybiscuits"
)

type HubManager struct {
	mod *Module

	lock       sync.Mutex
	url        string
	overlay    string
	artifactId string

	srcConn *riskybiscuits.DynamicSourceLSPConn
}

func NewHubManager(mod *Module) *HubManager {
	return &HubManager{
		mod: mod,
	}
}

func (hm *HubManager) Reset() {
	hm.lock.Lock()
	hm.resetLocked()
	hm.lock.Unlock()
}

func (hm *HubManager) resetLocked() {
	if conn := hm.srcConn; conn != nil {
		hm.srcConn = nil
		conn.Shutdown()
	}
	hm.url = ""
	hm.overlay = ""
	hm.artifactId = ""
}

func (hm *HubManager) Reload() {
	m := hm.mod.modFile.Custom["hub"]
	urlAny, overlayAny, artifactIdAny := m["url"], m["overlay"], m["artifactId"]

	url, ok := urlAny.(string)
	if !ok || url == "" {
		hm.Reset()
		return
	}

	overlay := ""
	ok = false
	switch overlayAny := overlayAny.(type) {
	case string:
		overlay = overlayAny
		ok = true
	case []byte:
		overlay = string(overlayAny)
		ok = true
	case []rune:
		overlay = string(overlayAny)
		ok = true
	}
	if !ok || overlay == "" {
		hm.Reset()
		return
	}

	artifactId, ok := artifactIdAny.(string)
	if !ok || artifactId == "" {
		hm.Reset()
		return
	}

	hm.lock.Lock()
	if url == hm.url && overlay == hm.overlay && artifactId == hm.artifactId {
		hm.lock.Unlock()
		return
	}

	hm.resetLocked()
	hm.url = url
	hm.overlay = overlay
	hm.artifactId = artifactId
	hm.lock.Unlock()

	go hm.dial(url, overlay, artifactId)
}

func (hm *HubManager) CreateSourceSnapshot(archiveWriter *zip.Writer) (err error) {
	mod := hm.mod
	w := mod.workspace
	w.inActor(func() {
		fs := w.overlayFS.IoFS(mod.rootURI.Path())
		w.debugLogf("HubManager: sending module snapshot")
		err = AddFS(archiveWriter, fs)
	})
	return err
}

func (hm *HubManager) SourceHasChanged() {
	hm.lock.Lock()
	srcConn := hm.srcConn
	hm.lock.Unlock()
	if srcConn != nil {
		hm.mod.workspace.debugLogf("HubManager: Signalling source has changed. Our srcId %v", srcConn.SrcId())
		srcConn.SignalSourceHasChanged()
	}
}

func (hm *HubManager) dial(url string, overlay string, artifactId string) {
	w := hm.mod.workspace
	ctx := context.Background()
	for ; true; time.Sleep(7 * time.Second) {
		hm.lock.Lock()
		unchanged := url == hm.url && overlay == hm.overlay && artifactId == hm.artifactId
		hm.lock.Unlock()
		if !unchanged {
			return
		}

		srcUrl := url + "/api/publish_fs?version=1"
		w.debugLogf("HubManager: connecting to %s", srcUrl)

		conn, _, err := websocket.Dial(ctx, srcUrl, nil)
		if err != nil {
			w.debugLogf("HubManager: unable to dial to %s: %v", srcUrl, err)
			continue
		}
		w.debugLogf("HubManager: connected to %s", srcUrl)

		srcConn, err := riskybiscuits.NewDynamicSourceLSPConn(conn, hm)
		if err != nil {
			w.debugLogf("HubManager: unable to connect to %s: %v", srcUrl, err)
			continue
		}
		srcId := srcConn.SrcId()
		w.debugLogf("HubManager: Connected to %s. Our srcId %v", srcUrl, srcId)

		hm.lock.Lock()
		unchanged = url == hm.url && overlay == hm.overlay && artifactId == hm.artifactId
		if unchanged {
			hm.srcConn = srcConn
		}
		hm.lock.Unlock()
		if !unchanged {
			srcConn.Shutdown()
			return
		}

		createPreviewURL := url + "/api/preview"
		bodyPost := strings.ReplaceAll(overlay, "srcId", `"`+srcId.String()+`"`)
		fmt.Println(bodyPost)
		previewResp, err := http.Post(createPreviewURL, "application/cue", bytes.NewBufferString(bodyPost))
		if err != nil || previewResp.StatusCode != 200 {
			body, _ := io.ReadAll(previewResp.Body)
			w.debugLogf("HubManager: could not create preview %s: %v %v: %v", createPreviewURL, err, previewResp.Status, string(body))
			srcConn.Shutdown()
			continue
		}
		body, err := io.ReadAll(previewResp.Body)
		if err != nil {
			w.debugLogf("HubManager: could not read body from create preview %s: %v", createPreviewURL, err)
			srcConn.Shutdown()
			continue
		}
		previewResp.Body.Close()
		var msg previewIdBodyMsg
		json.Unmarshal(body, &msg)
		previewId := msg.PreviewId
		w.debugLogf("HubManager: Preview created %s. %v", createPreviewURL, previewId)

		go hm.streamErrors(previewId, url, artifactId)

		srcConn.AwaitClosed()
		hm.lock.Lock()
		if hm.srcConn == srcConn {
			hm.srcConn = nil
		}
		hm.lock.Unlock()
	}
}

type previewIdBodyMsg struct {
	PreviewId string `json:"previewID"`
}

func (hm *HubManager) streamErrors(previewId string, url string, artifactId string) {
	hm.SourceHasChanged()
	mod := hm.mod
	w := mod.workspace
	rootUri := mod.rootURI
	for ; true; time.Sleep(7 * time.Second) {
		hm.lock.Lock()
		unchanged := url == hm.url && artifactId == hm.artifactId
		hm.lock.Unlock()
		if !unchanged {
			return
		}

		artifactUrl := url + "/api/watch/namespace/" + previewId + "/errors"
		w.debugLogf("HubManager: connecting to %s", artifactUrl)

		resp, err := http.Get(artifactUrl)
		if err != nil || resp.StatusCode != 200 {
			body, _ := io.ReadAll(resp.Body)
			w.debugLogf("HubManager: unable to dial to %s: %v %v %s", artifactUrl, err, resp.StatusCode, string(body))
			continue
		}
		w.debugLogf("HubManager: connected to %s", artifactUrl)

		scanner := bufio.NewScanner(resp.Body)

		byFile := make(map[*File][]error)
		for {
			data, err := readSSEEvent(scanner)
			if err != nil {
				w.debugLogf("sse decode error: %v", err)
				break
			}
			var errs NamespaceErrorStatus
			err = json.Unmarshal(data, &errs)
			if err != nil {
				w.debugLogf("decode json error: %v", err)
				break
			}

			w.debugLogf("live stream sent back %#v", errs)

			w.inActor(func() {
				for file := range byFile {
					byFile[file] = nil
				}

				var perrs []*riskybiscuits.PortableError
				for _, err := range errs.Artifacts {
					if re := err.RichError; re != nil {
						perrs = append(perrs, re)
					}
				}
				for _, err := range perrs {
					allPos := append([]riskybiscuits.PortablePosition{err.PositionJSON}, err.InputPositionsJSON...)
					for _, pos := range allPos {
						w.debugLogf("considering %#v", pos)
						filepath := strings.TrimPrefix(pos.Filepath, "/cuehub-src/lspSrc/")

						fileUri := rootUri + protocol.DocumentURI("/"+filepath)
						f, found := w.files[fileUri]
						if !found {
							w.debugLogf("HubManager: Unable to find file for error on %s", fileUri)
							continue
						}
						tokFile := f.tokFile
						if tokFile == nil {
							w.debugLogf("HubManager: Nil tokfile %s", fileUri)
							continue
						}
						se := &SimpleError{
							Portable: err,
							Pos:      tokFile.Pos(pos.Offset, token.NoRelPos),
						}
						byFile[f] = append(byFile[f], se)
					}

				}

				for file, errs := range byFile {
					file.ensureUser(mod, errs...)
					if len(errs) == 0 {
						delete(byFile, file)
					}
				}
				w.publishDiagnostics()
			})
		}
	}
}

func readSSEEvent(scanner *bufio.Scanner) ([]byte, error) {
	for scanner.Scan() {
		line := scanner.Text()
		// SSE data lines start with "data: "
		if data, wasCut := strings.CutPrefix(line, "data: "); wasCut {
			// Skip the blank line that follows
			scanner.Scan()
			return []byte(data), nil
		}
		// Skip empty lines and other SSE fields we don't use
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return nil, io.EOF
}

type NamespaceErrorStatus struct {
	HasError  bool                     `json:"hasError"`
	Artifacts map[string]ArtifactError `json:"artifacts,omitempty"`
}

type ArtifactError struct {
	Error     string                       `json:"error"`
	RichError *riskybiscuits.PortableError `json:",omitempty"`
}

type SimpleError struct {
	Portable *riskybiscuits.PortableError
	Pos      token.Pos
	InPos    []token.Pos
}

func (se *SimpleError) Position() token.Pos {
	return se.Pos
}

func (se *SimpleError) InputPositions() []token.Pos {
	return se.InPos
}

func (se *SimpleError) Error() string {
	return se.Portable.Error()
}

func (se *SimpleError) Path() []string {
	return se.Portable.Path()
}

func (se *SimpleError) Msg() (format string, args []interface{}) {
	return se.Portable.Msg()
}

func AddFS(w *zip.Writer, fsys fs.FS) error {
	return fs.WalkDir(fsys, ".", func(name string, d fs.DirEntry, err error) error {
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
}
