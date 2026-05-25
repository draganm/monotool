package ownership

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

func TestStripMarker(t *testing.T) {
	cases := []struct {
		name string
		path string
		in   string
		want string
	}{
		{"yaml without marker", "x.yaml", "apiVersion: v1\n", "apiVersion: v1\n"},
		{"yaml with marker", "x.yaml", "# monotool-hash: abc\napiVersion: v1\n", "apiVersion: v1\n"},
		{"yml with marker", "x.yml", "# monotool-hash: abc\nkind: X\n", "kind: X\n"},
		{"json unchanged", "x.json", `{"a":1}` + "\n", `{"a":1}` + "\n"},
		{"yaml only marker no body", "x.yaml", "# monotool-hash: abc\n", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := StripMarker(tc.path, []byte(tc.in))
			if string(got) != tc.want {
				t.Fatalf("StripMarker(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestComputeBodyHash(t *testing.T) {
	body := []byte("apiVersion: v1\nkind: ConfigMap\n")
	sum := sha256.Sum256(body)
	want := hex.EncodeToString(sum[:])

	// YAML with no marker: hash is over the whole input.
	gotYAML := ComputeBodyHash("x.yaml", body)
	if gotYAML != want {
		t.Fatalf("YAML no-marker hash = %s, want %s", gotYAML, want)
	}

	// YAML with marker: hash is over the body after the marker.
	gotMarked := ComputeBodyHash("x.yaml", append([]byte("# monotool-hash: deadbeef\n"), body...))
	if gotMarked != want {
		t.Fatalf("YAML marked hash = %s, want %s", gotMarked, want)
	}

	// JSON: hash is over the entire file body (no marker possible inline).
	gotJSON := ComputeBodyHash("x.json", body)
	if gotJSON != want {
		t.Fatalf("JSON hash = %s, want %s", gotJSON, want)
	}
}

func TestWriteMarkedYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "deploy.yaml")
	body := []byte("apiVersion: apps/v1\nkind: Deployment\n")

	if err := WriteMarked(path, body); err != nil {
		t.Fatalf("WriteMarked: %v", err)
	}

	written, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	wantHash := ComputeBodyHash(path, body)
	wantHead := MarkerPrefix + wantHash + "\n"
	if string(written[:len(wantHead)]) != wantHead {
		t.Fatalf("file head = %q, want %q", written[:len(wantHead)], wantHead)
	}
	if string(written[len(wantHead):]) != string(body) {
		t.Fatalf("file body after marker = %q, want %q", written[len(wantHead):], body)
	}
}

func TestWriteMarkedJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	body := []byte(`{"a":1}` + "\n")

	if err := WriteMarked(path, body); err != nil {
		t.Fatalf("WriteMarked: %v", err)
	}

	written, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile json: %v", err)
	}
	if string(written) != string(body) {
		t.Fatalf("JSON body modified: got %q, want %q", written, body)
	}

	sidecar, err := os.ReadFile(path + SidecarExt)
	if err != nil {
		t.Fatalf("ReadFile sidecar: %v", err)
	}
	wantHash := ComputeBodyHash(path, body)
	if string(sidecar) != wantHash+"\n" {
		t.Fatalf("sidecar = %q, want %q", sidecar, wantHash+"\n")
	}
}

func TestWriteMarkedCreatesDirs(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested/dir/x.yaml")
	if err := WriteMarked(path, []byte("a: b\n")); err != nil {
		t.Fatalf("WriteMarked nested: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("Stat: %v", err)
	}
}
