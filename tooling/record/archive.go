package record

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// JSONArchive persists one JSON RecordBundle file for each StableKey in a caller-owned directory.
type JSONArchive struct{ directory string }

// NewJSONArchive creates a JSON Archive rooted at an existing caller-owned directory.
func NewJSONArchive(directory string) (*JSONArchive, error) {
	if directory == "" {
		return nil, ErrInvalidConfig
	}
	info, err := os.Stat(directory)
	if err != nil || !info.IsDir() {
		return nil, fmt.Errorf("%w: archive directory", ErrInvalidConfig)
	}
	return &JSONArchive{directory: directory}, nil
}

// Save atomically replaces the JSON file for bundle.TargetKey after validating the whole bundle.
func (a *JSONArchive) Save(ctx context.Context, bundle RecordBundle) error {
	if a == nil {
		return ErrInvalidConfig
	}
	if err := usableContext(ctx); err != nil {
		return err
	}
	if err := validateBundle(bundle); err != nil {
		return err
	}
	payload, err := json.Marshal(cloneBundle(bundle))
	if err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidBundle, err)
	}
	if err := usableContext(ctx); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(a.directory, ".record-*.tmp")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if _, err := temporary.Write(payload); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := usableContext(ctx); err != nil {
		return err
	}
	return os.Rename(temporaryPath, a.pathFor(bundle.TargetKey))
}

// Load validates and returns an independent RecordBundle retained for key.
func (a *JSONArchive) Load(ctx context.Context, key StableKey) (RecordBundle, error) {
	if a == nil {
		return RecordBundle{}, ErrInvalidConfig
	}
	if err := usableContext(ctx); err != nil {
		return RecordBundle{}, err
	}
	if err := validateKey(key); err != nil {
		return RecordBundle{}, err
	}
	payload, err := os.ReadFile(a.pathFor(key))
	if errors.Is(err, os.ErrNotExist) {
		return RecordBundle{}, ErrRecordNotFound
	}
	if err != nil {
		return RecordBundle{}, err
	}
	var bundle RecordBundle
	if err := json.Unmarshal(payload, &bundle); err != nil {
		return RecordBundle{}, fmt.Errorf("%w: %v", ErrInvalidBundle, err)
	}
	if err := validateBundle(bundle); err != nil || bundle.TargetKey != key {
		return RecordBundle{}, ErrInvalidBundle
	}
	return cloneBundle(bundle), nil
}

func (a *JSONArchive) pathFor(key StableKey) string {
	digest := sha256.Sum256([]byte(key))
	return filepath.Join(a.directory, hex.EncodeToString(digest[:])+".json")
}
