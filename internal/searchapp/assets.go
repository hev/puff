package searchapp

import (
	"bytes"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"strings"
	"time"
)

// The upstream build can be embedded once approved for public distribution.
//
//go:embed assets/*
var embedded embed.FS

type Manifest struct {
	Protocol int               `json:"protocol"`
	Version  string            `json:"version"`
	Files    map[string]string `json:"files"`
}

// Assets verifies an upstream bundle and serves only the manifest's assets.
// Reading into memory also prevents later edits from bypassing verification.
func Assets(directory string) (fs.FS, error) {
	var source fs.FS
	if directory != "" {
		source = os.DirFS(directory)
	} else {
		sub, err := fs.Sub(embedded, "assets")
		if err != nil {
			return nil, err
		}
		source = sub
	}
	return VerifyAssets(source)
}

func VerifyAssets(source fs.FS) (fs.FS, error) {
	raw, err := fs.ReadFile(source, "manifest.json")
	if err != nil {
		return nil, errors.New("search UI bundle unavailable; build hev/search-ui's runtime and supply --ui-dir")
	}
	var manifest Manifest
	if len(raw) > 64<<10 || json.Unmarshal(raw, &manifest) != nil || manifest.Protocol != Protocol || manifest.Version == "" || len(manifest.Files) > 100 {
		return nil, errors.New("invalid UI manifest")
	}
	if _, ok := manifest.Files["index.html"]; !ok {
		return nil, errors.New("UI manifest must include index.html")
	}
	result := memoryFS{}
	total := 0
	for name, want := range manifest.Files {
		if !fs.ValidPath(name) || strings.HasPrefix(name, ".") || strings.Contains(name, "/.") || !strings.Contains(" .html .js .mjs .css .txt ", " "+path.Ext(name)+" ") {
			return nil, fmt.Errorf("invalid UI asset %q", name)
		}
		info, err := fs.Stat(source, name)
		if err != nil {
			return nil, err
		}
		if info.Size() > 5<<20 {
			return nil, errors.New("UI asset exceeds size limit")
		}
		data, err := fs.ReadFile(source, name)
		if err != nil {
			return nil, err
		}
		total += len(data)
		if total > 10<<20 {
			return nil, errors.New("UI bundle exceeds size limit")
		}
		sum := sha256.Sum256(data)
		if hex.EncodeToString(sum[:]) != want {
			return nil, fmt.Errorf("UI asset digest mismatch: %s", name)
		}
		result[name] = data
	}
	return result, nil
}

// Immutable manifest-scoped file system; no host-directory access at request time.
type memoryFS map[string][]byte

func (m memoryFS) Open(name string) (fs.File, error) {
	data, ok := m[name]
	if !ok {
		return nil, fs.ErrNotExist
	}
	return &memoryFile{Reader: bytes.NewReader(data), name: path.Base(name), size: int64(len(data))}, nil
}

type memoryFile struct {
	*bytes.Reader
	name string
	size int64
}

func (f *memoryFile) Close() error               { return nil }
func (f *memoryFile) Stat() (fs.FileInfo, error) { return fileInfo{name: f.name, size: f.size}, nil }

type fileInfo struct {
	name string
	size int64
}

func (f fileInfo) Name() string       { return f.name }
func (f fileInfo) Size() int64        { return f.size }
func (f fileInfo) Mode() fs.FileMode  { return 0444 }
func (f fileInfo) ModTime() time.Time { return time.Time{} }
func (f fileInfo) IsDir() bool        { return false }
func (f fileInfo) Sys() any           { return nil }

var _ io.ReadSeeker = (*memoryFile)(nil)
