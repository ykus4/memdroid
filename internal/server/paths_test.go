package server

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestConfine(t *testing.T) {
	root := t.TempDir()
	absRoot, err := filepath.Abs(root)
	if err != nil {
		t.Fatalf("abs root: %v", err)
	}

	tests := []struct {
		name    string
		root    string
		path    string
		want    string // expected result; ignored when wantErr
		wantErr string // substring the error must contain
	}{
		{
			name: "empty root passes relative path through",
			root: "",
			path: "state.json",
			want: "state.json",
		},
		{
			name: "empty root passes absolute path through",
			root: "",
			path: "/etc/passwd",
			want: "/etc/passwd",
		},
		{
			name: "relative name resolves under root",
			root: root,
			path: "state.json",
			want: filepath.Join(absRoot, "state.json"),
		},
		{
			name: "nested relative name resolves under root",
			root: root,
			path: "sub/dir/state.json",
			want: filepath.Join(absRoot, "sub", "dir", "state.json"),
		},
		{
			name: "dot-slash prefix is cleaned away",
			root: root,
			path: "./state.json",
			want: filepath.Join(absRoot, "state.json"),
		},
		{
			name: "empty path resolves to the root itself",
			root: root,
			path: "",
			want: absRoot,
		},
		{
			name:    "absolute path is rejected",
			root:    root,
			path:    "/etc/passwd",
			wantErr: "absolute paths are not allowed",
		},
		{
			name:    "absolute path inside the root is still rejected",
			root:    root,
			path:    filepath.Join(absRoot, "state.json"),
			wantErr: "absolute paths are not allowed",
		},
		// Traversal is reported, not silently rewritten: a caller who asks for
		// "../secrets.json" gets an error rather than a file quietly created
		// somewhere else under the root.
		{
			name:    "leading traversal is rejected",
			root:    root,
			path:    "../escape.json",
			wantErr: "escapes",
		},
		{
			name:    "deep traversal is rejected",
			root:    root,
			path:    "../../../../etc/passwd",
			wantErr: "escapes",
		},
		{
			name:    "interior traversal that climbs out is rejected",
			root:    root,
			path:    "a/b/../../../../escape.json",
			wantErr: "escapes",
		},
		{
			name: "traversal that stays inside resolves normally",
			root: root,
			path: "a/../b/state.json",
			want: filepath.Join(absRoot, "b", "state.json"),
		},
		{
			name: "relative root is made absolute",
			root: ".",
			path: "state.json",
			want: mustAbs(t, "state.json"),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := confine(tc.root, tc.path)
			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("confine(%q, %q) = %q, want error containing %q", tc.root, tc.path, got, tc.wantErr)
				}
				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("error = %v, want it to contain %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("confine(%q, %q): unexpected error: %v", tc.root, tc.path, err)
			}
			if got != tc.want {
				t.Fatalf("confine(%q, %q) = %q, want %q", tc.root, tc.path, got, tc.want)
			}
		})
	}
}

// TestConfineNeverEscapesRoot is the property the whole function exists for.
func TestConfineNeverEscapesRoot(t *testing.T) {
	root := t.TempDir()
	absRoot, err := filepath.Abs(root)
	if err != nil {
		t.Fatalf("abs root: %v", err)
	}

	paths := []string{
		"state.json",
		"../state.json",
		"../../../../../../../../etc/passwd",
		"a/../../b",
		"./../.././x",
		"..",
		"../",
		"sub/../../..",
		"\x00weird",
		"..\\windows-style",
	}
	for _, p := range paths {
		t.Run(p, func(t *testing.T) {
			got, err := confine(root, p)
			if err != nil {
				return // rejected outright is fine
			}
			rel, relErr := filepath.Rel(absRoot, got)
			if relErr != nil {
				t.Fatalf("Rel(%q, %q): %v", absRoot, got, relErr)
			}
			if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				t.Fatalf("confine(%q, %q) = %q, which escapes the root", root, p, got)
			}
		})
	}
}

func TestResolvePath(t *testing.T) {
	root := t.TempDir()
	absRoot, err := filepath.Abs(root)
	if err != nil {
		t.Fatalf("abs root: %v", err)
	}

	t.Run("falls back to the default name", func(t *testing.T) {
		h := &handler{fileRoot: root}
		rec := httptest.NewRecorder()
		got, ok := h.resolvePath(rec, "", "memdroid.json")
		if !ok {
			t.Fatalf("resolvePath failed: %s", rec.Body.String())
		}
		if want := filepath.Join(absRoot, "memdroid.json"); got != want {
			t.Errorf("resolvePath = %q, want %q", got, want)
		}
	})

	t.Run("writes a 400 on rejection", func(t *testing.T) {
		h := &handler{fileRoot: root}
		rec := httptest.NewRecorder()
		if _, ok := h.resolvePath(rec, "/etc/passwd", "memdroid.json"); ok {
			t.Fatal("resolvePath accepted an absolute path")
		}
		if rec.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", rec.Code)
		}
		if !strings.Contains(rec.Body.String(), "absolute paths are not allowed") {
			t.Errorf("body = %q, want it to explain the rejection", rec.Body.String())
		}
	})

	t.Run("empty root leaves the path untouched", func(t *testing.T) {
		h := &handler{}
		rec := httptest.NewRecorder()
		got, ok := h.resolvePath(rec, "/tmp/anywhere.json", "memdroid.json")
		if !ok {
			t.Fatalf("resolvePath failed: %s", rec.Body.String())
		}
		if got != "/tmp/anywhere.json" {
			t.Errorf("resolvePath = %q, want %q", got, "/tmp/anywhere.json")
		}
	})
}

func mustAbs(t *testing.T, p string) string {
	t.Helper()
	abs, err := filepath.Abs(p)
	if err != nil {
		t.Fatalf("Abs(%q): %v", p, err)
	}
	return abs
}
