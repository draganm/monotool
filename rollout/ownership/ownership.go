package ownership

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"strings"
)

// MarkerPrefix is the prefix of the YAML marker comment. The full line is
// MarkerPrefix + <hex sha256> + "\n".
const MarkerPrefix = "# monotool-hash: "

// SidecarExt is appended to a JSON file path to form its marker sidecar path.
const SidecarExt = ".monotool"

// isYAML reports whether path has a YAML extension.
func isYAML(path string) bool {
	ext := filepath.Ext(path)
	return ext == ".yaml" || ext == ".yml"
}

// isJSON reports whether path has a JSON extension.
func isJSON(path string) bool {
	return filepath.Ext(path) == ".json"
}

// hasMarkerLine reports whether body starts with the YAML marker prefix.
func hasMarkerLine(body []byte) bool {
	return strings.HasPrefix(string(body), MarkerPrefix)
}

// StripMarker returns the portion of body that participates in the hash.
// For YAML files with a marker line as the first line, the marker line and its
// trailing newline are removed. All other inputs are returned unchanged.
func StripMarker(path string, body []byte) []byte {
	if !isYAML(path) || !hasMarkerLine(body) {
		return body
	}
	nl := bytes.IndexByte(body, '\n')
	if nl < 0 {
		return nil
	}
	return body[nl+1:]
}

// ComputeBodyHash returns the lowercase-hex SHA-256 of the hash-relevant
// portion of body (see StripMarker).
func ComputeBodyHash(path string, body []byte) string {
	stripped := StripMarker(path, body)
	sum := sha256.Sum256(stripped)
	return hex.EncodeToString(sum[:])
}
