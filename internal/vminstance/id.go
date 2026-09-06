// Package vminstance resolves human VM instance IDs to opaque backend IDs.
package vminstance

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

var validID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$`)

// Resolve validates id (empty → "default") and derives a stable backend VM ID
// from project root, config path, and instance ID.
func Resolve(projectRoot, configPath, id string) (instanceID, backendID string, err error) {
	instanceID, err = Normalize(id)
	if err != nil {
		return "", "", err
	}
	projectRoot, err = filepath.Abs(projectRoot)
	if err != nil {
		return "", "", err
	}
	configPath, err = filepath.Abs(configPath)
	if err != nil {
		return "", "", err
	}
	sum := sha256.Sum256([]byte(projectRoot + "\x00" + configPath + "\x00" + instanceID))
	return instanceID, "cage-" + hex.EncodeToString(sum[:12]), nil
}

// Normalize trims id; empty becomes "default". Invalid IDs return an error.
func Normalize(id string) (string, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return "default", nil
	}
	if !validID.MatchString(id) {
		return "", fmt.Errorf("invalid VM instance ID %q", id)
	}
	return id, nil
}
