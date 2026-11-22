package embed

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewLocalProvider(t *testing.T) {
	e, err := New(ProviderLocal, "")
	if err != nil {
		t.Fatalf("New(local): %v", err)
	}
	if _, ok := e.(*localEmbedder); !ok {
		t.Fatalf("New(local) = %T, want *localEmbedder", e)
	}
	if got := e.Dim(); got != Dim {
		t.Errorf("Dim() = %d, want %d", got, Dim)
	}
	if got := e.Version(); got != LocalVersion {
		t.Errorf("Version() = %q, want %q", got, LocalVersion)
	}
}

// The local provider ignores modelRef; its version stays the hashing key.
func TestNewLocalProviderIgnoresModelRef(t *testing.T) {
	e, err := New(ProviderLocal, "intfloat/multilingual-e5-small")
	if err != nil {
		t.Fatalf("New(local): %v", err)
	}
	if got := e.Version(); got != LocalVersion {
		t.Errorf("Version() = %q, want %q", got, LocalVersion)
	}
}

func TestNewUnknownProvider(t *testing.T) {
	cases := []string{"", "foo", "LOCAL", "sentence-transformers"}
	for _, provider := range cases {
		t.Run(provider, func(t *testing.T) {
			_, err := New(provider, "")
			if err == nil {
				t.Fatal("New(unknown): want error, got nil")
			}
			if provider != "" && !strings.Contains(err.Error(), provider) {
				t.Errorf("error should name the rejected provider %q, got: %v", provider, err)
			}
			if !strings.Contains(err.Error(), ProviderLocal) || !strings.Contains(err.Error(), ProviderRembed) {
				t.Errorf("error should list valid providers, got: %v", err)
			}
		})
	}
}

func TestResolveRembedRef(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"empty defaults", "", DefaultRembedRef},
		{"whitespace defaults", "   ", DefaultRembedRef},
		{"trims surrounding space", "  org/name  ", "org/name"},
		{"passes through", "org/name", "org/name"},
		{"keeps hf prefix", "hf:org/name", "hf:org/name"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := resolveRembedRef(tc.in); got != tc.want {
				t.Errorf("resolveRembedRef(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestRembedVersion(t *testing.T) {
	cases := []struct {
		ref  string
		want string
	}{
		{"org/name", "rembed:org/name#" + RembedDomainVersion},
		{DefaultRembedRef, "rembed:" + DefaultRembedRef + "#" + RembedDomainVersion},
	}
	for _, tc := range cases {
		t.Run(tc.ref, func(t *testing.T) {
			if got := rembedVersion(tc.ref); got != tc.want {
				t.Errorf("rembedVersion(%q) = %q, want %q", tc.ref, got, tc.want)
			}
		})
	}
	// The Embedder's Version() reports the same key derived from the ref.
	e := &rembedEmbedder{version: rembedVersion("org/name")}
	if got := e.Version(); got != "rembed:org/name#"+RembedDomainVersion {
		t.Errorf("Version() = %q, want %q", got, "rembed:org/name#"+RembedDomainVersion)
	}
}

// The rembed branch is selected without touching the network: an invalid local
// ref fails inside rembed.Load (no download), and the error carries the ref.
func TestNewRembedInvalidRefOffline(t *testing.T) {
	t.Setenv("HF_HOME", "")
	t.Setenv("REMBED_CACHE", "")
	ref := filepath.Join(t.TempDir(), "missing-model")
	_, err := New(ProviderRembed, ref)
	if err == nil {
		t.Fatal("New(rembed, invalid ref): want error, got nil")
	}
	if !strings.Contains(err.Error(), ref) {
		t.Errorf("error should carry the model ref %q, got: %v", ref, err)
	}
}

// When neither cache variable is configured, newRembed sets REMBED_CACHE to
// <user cache dir>/rembed before Load. The ref is an invalid local path so
// Load fails offline; the side effect is what this test asserts.
func TestNewRembedConfiguresCache(t *testing.T) {
	t.Setenv("HF_HOME", "")
	t.Setenv("REMBED_CACHE", "")
	ref := filepath.Join(t.TempDir(), "missing-model")
	if _, err := New(ProviderRembed, ref); err == nil {
		t.Fatal("New(rembed, invalid ref): expected an offline Load error")
	}
	cache, err := os.UserCacheDir()
	if err != nil {
		t.Fatalf("os.UserCacheDir: %v", err)
	}
	want := filepath.Join(cache, "rembed")
	if got := os.Getenv("REMBED_CACHE"); got != want {
		t.Errorf("REMBED_CACHE = %q, want %q", got, want)
	}
}

// An already-configured cache must be left untouched.
func TestNewRembedRespectsExistingCache(t *testing.T) {
	t.Setenv("HF_HOME", "")
	t.Setenv("REMBED_CACHE", "/custom/cache")
	ref := filepath.Join(t.TempDir(), "missing-model")
	_, _ = New(ProviderRembed, ref)
	if got := os.Getenv("REMBED_CACHE"); got != "/custom/cache" {
		t.Errorf("REMBED_CACHE = %q, want %q (existing value must be preserved)", got, "/custom/cache")
	}
}
