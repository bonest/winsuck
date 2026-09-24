package transfer

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

const manifestName = ".winsuck-manifest.json"

type FileState struct {
	Size  int64 `json:"size"`
	MTime int64 `json:"mtime_unix"`
}

type Manifest map[string]FileState

func ReadManifest(destination string) (Manifest, error) {
	data, err := os.ReadFile(filepath.Join(destination, manifestName))
	if os.IsNotExist(err) {
		return Manifest{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read manifest: %w", err)
	}
	manifest := Manifest{}
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("parse manifest: %w", err)
	}
	return manifest, nil
}

func WriteManifest(destination string, manifest Manifest) error {
	data, err := json.Marshal(manifest)
	if err != nil {
		return fmt.Errorf("encode manifest: %w", err)
	}
	temporary, err := os.CreateTemp(destination, ".winsuck-manifest-*")
	if err != nil {
		return fmt.Errorf("create temporary manifest: %w", err)
	}
	name := temporary.Name()
	defer os.Remove(name)
	if _, err := temporary.Write(data); err != nil {
		temporary.Close()
		return fmt.Errorf("write manifest: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close manifest: %w", err)
	}
	if err := os.Rename(name, filepath.Join(destination, manifestName)); err != nil {
		return fmt.Errorf("replace manifest: %w", err)
	}
	return nil
}
