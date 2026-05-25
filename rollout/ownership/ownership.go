package ownership

import (
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
