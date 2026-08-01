package server

import (
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
)

// resolvePath turns a client-supplied filename into a path this process is
// allowed to touch.
//
// Several endpoints read or write host files (session save/load, .CT import,
// region dump). Because the API can be bound to a non-loopback address, an
// unrestricted path would let a remote caller read or overwrite anything the
// user can — so paths are confined to fileRoot unless the operator explicitly
// disabled the restriction.
func (h *handler) resolvePath(w http.ResponseWriter, path, def string) (string, bool) {
	if path == "" {
		path = def
	}
	resolved, err := confine(h.fileRoot, path)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return "", false
	}
	return resolved, true
}

// confine resolves path against root and rejects anything that escapes it. An
// empty root disables the check.
//
// Traversal is rejected outright rather than silently rewritten: a caller who
// asks for "../secrets.json" should get an error, not a file quietly created
// somewhere else under root.
func confine(root, path string) (string, error) {
	if root == "" {
		return path, nil
	}
	if filepath.IsAbs(path) {
		return "", fmt.Errorf("absolute paths are not allowed; give a name relative to %s", root)
	}

	cleaned := filepath.Clean(path)
	if cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path %q escapes %s", path, root)
	}

	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("resolve file root: %w", err)
	}
	full := filepath.Join(absRoot, cleaned)

	// Check containment against the symlink-resolved forms — a link inside the
	// root can still point outside it — but return the unresolved path, so the
	// caller gets back something recognisably under the root they configured.
	if !within(absRoot, full) {
		return "", fmt.Errorf("path %q escapes %s", path, root)
	}
	return full, nil
}

// within reports whether full lands inside root once symlinks are resolved.
// Both sides are resolved so the comparison happens in one namespace.
func within(root, full string) bool {
	rel, err := filepath.Rel(resolveExisting(root), resolveExisting(full))
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// resolveExisting resolves symlinks as far down path as actually exists, then
// re-appends the rest. A plain EvalSymlinks is no good here: the target of a
// save usually does not exist yet, and neither may the directories leading to
// it.
func resolveExisting(path string) string {
	rest := ""
	for cur := path; ; {
		if resolved, err := filepath.EvalSymlinks(cur); err == nil {
			return filepath.Join(resolved, rest)
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return path // nothing along the way exists
		}
		rest = filepath.Join(filepath.Base(cur), rest)
		cur = parent
	}
}
