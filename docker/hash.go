package docker

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"

	"github.com/moby/patternmatcher"
	"github.com/moby/patternmatcher/ignorefile"
)

// HashBuildContext computes a deterministic 8-byte hex tag for a Docker build
// context directory and its Dockerfile. Files excluded by a .dockerignore inside
// the context are not hashed, matching Docker/BuildKit's own view of the context.
// The .dockerignore file itself is hashed separately so that changing the rules
// still changes the tag, and the Dockerfile is hashed explicitly so that a
// Dockerfile living outside the context is also accounted for.
func HashBuildContext(contextDir, dockerfilePath string) (string, error) {
	info, err := os.Stat(contextDir)
	if err != nil {
		return "", fmt.Errorf("stat context dir %s: %w", contextDir, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("context path is not a directory: %s", contextDir)
	}

	h := sha256.New()

	// 1. Dockerfile contents.
	dfContent, err := os.ReadFile(dockerfilePath)
	if err != nil {
		return "", fmt.Errorf("read dockerfile %s: %w", dockerfilePath, err)
	}
	dfSum := sha256.Sum256(dfContent)
	fmt.Fprintf(h, "dockerfile\x00%x\n", dfSum)

	// 2. .dockerignore: hash contents explicitly and build a matcher.
	var matcher *patternmatcher.PatternMatcher
	dockerignorePath := filepath.Join(contextDir, ".dockerignore")
	diContent, err := os.ReadFile(dockerignorePath)
	switch {
	case err == nil:
		diSum := sha256.Sum256(diContent)
		fmt.Fprintf(h, "dockerignore\x00%x\n", diSum)

		f, err := os.Open(dockerignorePath)
		if err != nil {
			return "", fmt.Errorf("open .dockerignore: %w", err)
		}
		patterns, perr := ignorefile.ReadAll(f)
		f.Close()
		if perr != nil {
			return "", fmt.Errorf("parse .dockerignore: %w", perr)
		}
		matcher, err = patternmatcher.New(patterns)
		if err != nil {
			return "", fmt.Errorf("build .dockerignore matcher: %w", err)
		}
	case os.IsNotExist(err):
		// no .dockerignore — nothing to exclude
	default:
		return "", fmt.Errorf("read .dockerignore: %w", err)
	}

	// 3. Walk the context directory, collecting surviving entries.
	type entry struct {
		rel string
		d   fs.DirEntry
	}
	var entries []entry

	walkErr := filepath.WalkDir(contextDir, func(p string, d fs.DirEntry, werr error) error {
		if werr != nil {
			return werr
		}
		if p == contextDir {
			return nil
		}
		rel, err := filepath.Rel(contextDir, p)
		if err != nil {
			return fmt.Errorf("rel %s: %w", p, err)
		}
		rel = filepath.ToSlash(rel)

		if matcher != nil {
			matched, err := matcher.MatchesOrParentMatches(rel)
			if err != nil {
				return fmt.Errorf("match %s: %w", rel, err)
			}
			if matched {
				// If the entry is a directory and there are no negation patterns
				// that could re-include children, prune the subtree. Otherwise
				// descend so negations can still take effect.
				if d.IsDir() && !matcher.Exclusions() {
					return filepath.SkipDir
				}
				return nil
			}
		}

		entries = append(entries, entry{rel: rel, d: d})
		return nil
	})
	if walkErr != nil {
		return "", fmt.Errorf("walk context: %w", walkErr)
	}

	// 4. Sort by relative path (defensive — WalkDir is already lexical).
	sort.Slice(entries, func(i, j int) bool { return entries[i].rel < entries[j].rel })

	// 5. Feed each entry into the running hash.
	for _, e := range entries {
		einfo, err := e.d.Info()
		if err != nil {
			return "", fmt.Errorf("stat %s: %w", e.rel, err)
		}
		mode := einfo.Mode()

		switch {
		case mode.IsRegular():
			content, err := os.ReadFile(filepath.Join(contextDir, e.rel))
			if err != nil {
				return "", fmt.Errorf("read %s: %w", e.rel, err)
			}
			contentSum := sha256.Sum256(content)
			fmt.Fprintf(h, "file\x00%s\x00%o\x00%x\n", e.rel, mode.Perm(), contentSum)
		case mode&fs.ModeSymlink != 0:
			target, err := os.Readlink(filepath.Join(contextDir, e.rel))
			if err != nil {
				return "", fmt.Errorf("readlink %s: %w", e.rel, err)
			}
			fmt.Fprintf(h, "link\x00%s\x00%s\n", e.rel, target)
		case mode.IsDir():
			// Directories contribute no entry; their children carry the signal.
		default:
			return "", fmt.Errorf("unsupported file type in build context: %s (mode %s)", e.rel, mode)
		}
	}

	sum := h.Sum(nil)
	return hex.EncodeToString(sum[:8]), nil
}
