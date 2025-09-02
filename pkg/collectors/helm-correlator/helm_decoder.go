package helmcorrelator

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"go.uber.org/zap"
	v1 "k8s.io/api/core/v1"
)

// HelmSecretDecoder decodes Helm release secrets
// We decode without using Helm SDK to avoid heavy dependencies
type HelmSecretDecoder struct {
	logger *zap.Logger
}

// NewHelmSecretDecoder creates a new decoder
func NewHelmSecretDecoder(logger *zap.Logger) *HelmSecretDecoder {
	return &HelmSecretDecoder{
		logger: logger,
	}
}

// DecodeSecret decodes a Helm release secret into our simplified format
func (d *HelmSecretDecoder) DecodeSecret(secret *v1.Secret) (*HelmRelease, error) {
	// Check if this is a Helm release secret
	if !d.isHelmSecret(secret) {
		return nil, fmt.Errorf("not a Helm release secret")
	}

	// Get the release data
	releaseData, exists := secret.Data["release"]
	if !exists {
		return nil, fmt.Errorf("no 'release' data in secret")
	}

	// Decode the release
	release, err := d.decodeRelease(releaseData)
	if err != nil {
		return nil, fmt.Errorf("failed to decode release: %w", err)
	}

	// Set namespace from secret
	release.Namespace = secret.Namespace
	return release, nil
}

// isHelmSecret checks if a secret is a Helm release
func (d *HelmSecretDecoder) isHelmSecret(secret *v1.Secret) bool {
	// Check secret type
	if secret.Type != "helm.sh/release.v1" {
		return false
	}

	// Check naming pattern
	return strings.HasPrefix(secret.Name, "sh.helm.release.v1.")
}

// decodeRelease decodes the base64+gzip+json release data
func (d *HelmSecretDecoder) decodeRelease(data []byte) (*HelmRelease, error) {
	// Base64 decode
	decoded, err := base64.StdEncoding.DecodeString(string(data))
	if err != nil {
		return nil, fmt.Errorf("failed to base64 decode: %w", err)
	}

	// Gunzip
	reader, err := gzip.NewReader(bytes.NewReader(decoded))
	if err != nil {
		return nil, fmt.Errorf("failed to create gzip reader: %w", err)
	}
	defer reader.Close()

	uncompressed, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("failed to gunzip: %w", err)
	}

	// Parse as JSON (simplified - we don't need full protobuf)
	var releaseData json.RawMessage
	if err := json.Unmarshal(uncompressed, &releaseData); err != nil {
		// If JSON fails, try to extract basic info from the data
		// Helm uses protobuf, but we can extract key fields
		return d.extractBasicInfo(uncompressed), nil
	}

	return d.parseReleaseJSON(releaseData), nil
}

// extractBasicInfo extracts basic info from protobuf data without full decode
func (d *HelmSecretDecoder) extractBasicInfo(data []byte) *HelmRelease {
	release := &HelmRelease{
		Status:    "unknown",
		Version:   0,
		CreatedAt: time.Now(),
	}

	// Try to extract status (common strings in protobuf)
	dataStr := string(data)
	if strings.Contains(dataStr, "deployed") {
		release.Status = "deployed"
	} else if strings.Contains(dataStr, "failed") {
		release.Status = "failed"
	} else if strings.Contains(dataStr, "pending") {
		release.Status = "pending"
	} else if strings.Contains(dataStr, "superseded") {
		release.Status = "superseded"
	}

	// Try to extract name
	if idx := strings.Index(dataStr, "name:"); idx != -1 {
		end := strings.IndexAny(dataStr[idx+5:], "\x00\n\"")
		if end > 0 {
			release.Name = strings.TrimSpace(dataStr[idx+5 : idx+5+end])
		}
	}

	return release
}

// parseReleaseJSON parses a release from raw JSON data
func (d *HelmSecretDecoder) parseReleaseJSON(data json.RawMessage) *HelmRelease {
	// Define a temporary struct for unmarshaling
	var temp struct {
		Name      string      `json:"name"`
		Namespace string      `json:"namespace"`
		Version   interface{} `json:"version"` // Can be int or float
		Status    string      `json:"status"`
		Manifest  string      `json:"manifest"`
		Info      struct {
			Status      string `json:"status"`
			Description string `json:"description"`
			Notes       string `json:"notes"`
		} `json:"info"`
		Chart struct {
			Metadata struct {
				Name       string `json:"name"`
				Version    string `json:"version"`
				AppVersion string `json:"appVersion"`
			} `json:"metadata"`
		} `json:"chart"`
		Config json.RawMessage `json:"config"`
	}

	// Try to unmarshal into temp struct
	json.Unmarshal(data, &temp)

	// Convert version to int
	version := 0
	if temp.Version != nil {
		switch v := temp.Version.(type) {
		case float64:
			version = int(v)
		case int:
			version = v
		case int64:
			version = int(v)
		}
	}

	release := &HelmRelease{
		Name:      temp.Name,
		Namespace: temp.Namespace,
		Version:   version,
		Status:    temp.Status,
		Manifest:  temp.Manifest,
		CreatedAt: time.Now(),
	}

	// If status is not set at top level, check info.status
	if release.Status == "" && temp.Info.Status != "" {
		release.Status = temp.Info.Status
	}

	// Set info if present
	if temp.Info.Description != "" || temp.Info.Notes != "" || temp.Info.Status != "" {
		release.Info = &ReleaseInfo{
			Description: temp.Info.Description,
			Notes:       temp.Info.Notes,
			Status:      temp.Info.Status,
		}
	}

	// Set chart info
	if temp.Chart.Metadata.Name != "" {
		if temp.Chart.Metadata.Version != "" {
			release.Chart = fmt.Sprintf("%s-%s", temp.Chart.Metadata.Name, temp.Chart.Metadata.Version)
		}
		release.AppVersion = temp.Chart.Metadata.AppVersion
	}

	// Store raw values
	if len(temp.Config) > 0 {
		release.Values = &HelmValues{
			Raw: temp.Config,
		}
	}

	return release
}

// ParseHelmSecretName extracts release name and version from secret name
func (d *HelmSecretDecoder) ParseHelmSecretName(name string) (string, int) {
	// Format: sh.helm.release.v1.RELEASE_NAME.vVERSION
	parts := strings.Split(name, ".")
	if len(parts) < 6 {
		return "", 0
	}

	// Get release name (everything between v1. and .v)
	releaseParts := []string{}
	versionIdx := -1

	for i := 4; i < len(parts); i++ {
		if strings.HasPrefix(parts[i], "v") && i == len(parts)-1 {
			versionIdx = i
			break
		}
		releaseParts = append(releaseParts, parts[i])
	}

	if versionIdx == -1 {
		return "", 0
	}

	releaseName := strings.Join(releaseParts, ".")

	// Parse version
	versionStr := strings.TrimPrefix(parts[versionIdx], "v")
	version, _ := strconv.Atoi(versionStr)

	return releaseName, version
}

// CompareReleases compares two releases and returns changes
func (d *HelmSecretDecoder) CompareReleases(old, new *HelmRelease) []string {
	changes := []string{}

	if old.Status != new.Status {
		changes = append(changes, fmt.Sprintf("Status: %s → %s", old.Status, new.Status))
	}

	if old.Version != new.Version {
		changes = append(changes, fmt.Sprintf("Version: %d → %d", old.Version, new.Version))
	}

	if old.Chart != new.Chart {
		changes = append(changes, fmt.Sprintf("Chart: %s → %s", old.Chart, new.Chart))
	}

	// Check values changes by comparing raw JSON
	if !d.valuesEqual(old.Values, new.Values) {
		changes = append(changes, "Values changed")
	}

	// Check manifest size change
	if len(old.Manifest) != len(new.Manifest) {
		changes = append(changes, fmt.Sprintf("Manifest size: %d → %d bytes",
			len(old.Manifest), len(new.Manifest)))
	}

	return changes
}

// valuesEqual compares two HelmValues
func (d *HelmSecretDecoder) valuesEqual(a, b *HelmValues) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}

	// Compare raw JSON bytes
	return string(a.Raw) == string(b.Raw)
}

// ExtractFailureInfo extracts failure information from a release
func (d *HelmSecretDecoder) ExtractFailureInfo(release *HelmRelease) FailureInfo {
	info := FailureInfo{
		Status:    release.Status,
		Timestamp: time.Now(),
	}

	// Check if it's a failure state
	failedStates := []string{"failed", "pending-upgrade", "pending-install",
		"pending-rollback", "uninstalling", "superseded"}

	for _, state := range failedStates {
		if strings.Contains(strings.ToLower(release.Status), state) {
			info.HasError = true
			break
		}
	}

	// Extract description
	if release.Info != nil {
		info.Description = release.Info.Description
		info.Notes = release.Info.Notes
	}

	// Extract hook information
	for _, hook := range release.Hooks {
		if strings.Contains(hook.Events, "failed") {
			info.HookFailures = append(info.HookFailures, hook.Name)
		}
	}

	return info
}
