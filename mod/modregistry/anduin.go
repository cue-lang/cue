// Copyright (C) 2014-2024 Anduin Transactions Inc.
//
// Anduin maintained source code to patch OCI client behavior

package modregistry

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

var logging, _ = strconv.ParseBool(os.Getenv("ANDUIN_CUE_DEBUG"))

type anduinPatch struct {
	originalClient *Client
}

// repackZipFile
// Override default put module function to make restructure OCI layer. The goal is to make it compatible with both Oras and Cue cli
// The expected layers are as following:
//  1. All file with annotations of oras
//  2. ZIP file with only *.cue file included (compatible layer with cue)
//  3. Cue module file (compatible layer with cue)
//
// Note: *.cue will be duplicated because Oras will not pull cue layers
// Function should return a list of oras layer descriptors
func (p *anduinPatch) repackZipFile(repackZip *os.File, ctx context.Context, m *checkedModule) ([]ocispec.Descriptor, error) {
	logf("using patched `putCheckedModule`")

	loc, err := p.originalClient.resolve(m.mv)
	if err != nil {
		return nil, err
	}

	zw := zip.NewWriter(repackZip)

	// oras only layers
	orasLayers := []ocispec.Descriptor{}

	logf("valid files: %v", m.validFiles)
	for _, zf := range m.zipr.File {
		// only handle valid file
		if !slices.Contains(m.validFiles, zf.Name) {
			continue
		}

		err := func() error {
			rc, err := zf.Open()
			if err != nil {
				return err
			}
			defer rc.Close()

			// single buffer for multiple copy operation
			// avoid re-reading zip file
			data, err := io.ReadAll(rc)
			if err != nil {
				return err
			}

			// only add cue file to repack zip
			if strings.HasSuffix(zf.Name, ".cue") {
				if err := addFileToRepack(zw, zf.Name, bytes.NewReader(data)); err != nil {
					return err
				}
			}

			annotation := map[string]string{}
			annotation[ocispec.AnnotationTitle] = zf.Name
			dataLayer := ocispec.Descriptor{
				Digest:      digest.FromBytes(data),
				MediaType:   ocispec.MediaTypeImageLayer,
				Size:        int64(len(data)),
				Annotations: annotation,
			}
			if _, err := loc.Registry.PushBlob(ctx, loc.Repository, dataLayer, bytes.NewReader(data)); err != nil {
				return fmt.Errorf("cannot push oras data layer: %v", err)
			}
			orasLayers = append(orasLayers, dataLayer)

			return nil
		}()
		if err != nil {
			return nil, err
		}

	}

	if err := zw.Close(); err != nil {
		return nil, fmt.Errorf("cannot flush repack zip file: %v", err)
	}
	if _, err := repackZip.Seek(0, io.SeekStart); err != nil {
		return nil, fmt.Errorf("cannot rewind repack zip file: %v", err)
	}
	fileStat, err := repackZip.Stat()
	if err != nil {
		return nil, fmt.Errorf("cannot calculate repack zip file stat: %v", err)
	}
	zr, err := zip.NewReader(repackZip, fileStat.Size())
	if err != nil {
		return nil, fmt.Errorf("cannot create repack file reader: %v", err)
	}

	logf("total repack zip file: %d", fileStat.Size())
	// update checkedModule
	m.blobr = repackZip
	m.size = fileStat.Size()
	m.zipr = zr

	return orasLayers, nil
}

func addFileToRepack(zr *zip.Writer, name string, r io.Reader) error {
	w, err := zr.Create(name)
	if err != nil {
		return err
	}
	_, err = io.Copy(w, r)
	return err
}

func zipContentLayer(m *ocispec.Manifest) ocispec.Descriptor {
	layerLen := len(m.Layers)
	return m.Layers[layerLen-2]
}

func moduleFileLayer(m *ocispec.Manifest) ocispec.Descriptor {
	layerLen := len(m.Layers)
	return m.Layers[layerLen-1]
}

func logf(f string, a ...any) {
	if logging {
		log.Printf("anduin: "+f, a...)
	}
}
