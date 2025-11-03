package cache

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"cuelang.org/go/cue/token"
	"cuelang.org/go/internal/golangorgx/gopls/protocol"
	"github.com/coder/websocket"
	"github.com/google/uuid"
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

func (hm *HubManager) Shutdown() {
	hm.lock.Lock()
	hm.shutdownLocked()
	hm.lock.Unlock()
}

func (hm *HubManager) shutdownLocked() {
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
		hm.Shutdown()
		return
	}
	overlay, ok := overlayAny.(string)
	if !ok || overlay == "" {
		hm.Shutdown()
		return
	}
	artifactId, ok := artifactIdAny.(string)
	if !ok || artifactId == "" {
		hm.Shutdown()
		return
	}

	hm.lock.Lock()
	if url == hm.url && overlay == hm.overlay && artifactId == hm.artifactId {
		hm.lock.Unlock()
		return
	}

	hm.shutdownLocked()
	hm.url = url
	hm.overlay = overlay
	hm.artifactId = artifactId
	hm.lock.Unlock()

	go hm.dial(url, overlay, artifactId)
}

func (hm *HubManager) CreateSourceSnapshot(archiveWriter *zip.Writer) error {
	mod := hm.mod
	w := mod.workspace
	fs := w.overlayFS.IoFS(mod.rootURI.Path())
	w.debugLogf("HubManager: sending module snapshot")
	return archiveWriter.AddFS(fs)
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

		srcUrl := url + "/fs_sources"
		w.debugLogf("HubManager: connecting to %s", srcUrl)

		conn, _, err := websocket.Dial(ctx, srcUrl, nil)
		if err != nil {
			w.debugLogf("HubManager: unable to dial to %s: %v", srcUrl, err)
			continue
		}
		w.debugLogf("HubManager: connected to %s: %v", srcUrl, conn)

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

		createPreviewURL := url + "/api/previewOverlay/latest?live=bool"
		previewResp, err := http.Post(createPreviewURL, "application/cue",
			bytes.NewBufferString(strings.ReplaceAll(overlay, "srcId", `"`+srcId.String()+`"`)))
		if err != nil || previewResp.StatusCode != 200 {
			w.debugLogf("HubManager: could not create preview %s: %v %v", createPreviewURL, err, previewResp.Status)
			continue
		}
		body, err := io.ReadAll(previewResp.Body)
		if err != nil {
			w.debugLogf("HubManager: could not read body from create preview %s: %v", createPreviewURL, err)
			continue
		}
		previewResp.Body.Close()
		previewId, err := uuid.ParseBytes(body)
		if err != nil {
			w.debugLogf("HubManager: could not parse uuid from create preview %s, %v", createPreviewURL, err)
			continue
		}
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

func (hm *HubManager) streamErrors(previewId uuid.UUID, url string, artifactId string) {
	mod := hm.mod
	w := mod.workspace
	rootUri := mod.rootURI
	ctx := context.Background()
	for ; true; time.Sleep(7 * time.Second) {
		hm.lock.Lock()
		unchanged := url == hm.url && artifactId == hm.artifactId
		hm.lock.Unlock()
		if !unchanged {
			return
		}

		artifactUrl := url + "/api/livePreviewErrors/" + previewId.String() + "?artifact=" + artifactId
		w.debugLogf("HubManager: connecting to %s", artifactUrl)

		conn, _, err := websocket.Dial(ctx, artifactUrl, nil)
		if err != nil {
			w.debugLogf("HubManager: unable to dial to %s: %v", artifactUrl, err)
			continue
		}
		w.debugLogf("HubManager: connected to %s: %v", artifactUrl, conn)

		for {
			msgType, data, err := conn.Read(ctx)
			if err != nil {
				switch websocket.CloseStatus(err) {
				case websocket.StatusNormalClosure,
					websocket.StatusGoingAway:
				default:
					w.debugLogf("conn.Read: %v; status %v\n", err, websocket.CloseStatus(err))
				}
				conn.Close(websocket.StatusNormalClosure, "")
				break
			} else if msgType != websocket.MessageText {
				w.debugLog("Not a text message")
				conn.Close(websocket.StatusNormalClosure, "")
				break
			}
			var errs ErrorMsg
			if err = json.Unmarshal(data, &errs); err != nil {
				w.debugLog(err.Error())
				conn.Close(websocket.StatusNormalClosure, "")
			}
			byFile := make(map[*File][]error)
			for _, err := range errs.Errors {
				filePath := err.PositionJSON.Filepath
				fileUri := rootUri + protocol.DocumentURI("/"+filePath)
				w.filesMutex.Lock()
				f, found := w.files[fileUri]
				w.filesMutex.Unlock()
				if !found {
					w.debugLogf("HubManager: Unable to find file for error on %s", fileUri)
					continue
				}
				byFile[f] = append(byFile[f], err)
			}
			for file, errs := range byFile {
				file.ensureUser(mod, errs...)
			}
		}
	}
}

type ErrorMsg struct {
	Errors []*token.PortableError `json:"errors"`
}
