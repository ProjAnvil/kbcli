package main

import "testing"

func TestEffectiveVersion(t *testing.T) {
	orig := version
	t.Cleanup(func() { version = orig })

	t.Run("ldflags value wins", func(t *testing.T) {
		version = "v9.8.7-injected"
		if got := effectiveVersion(); got != "v9.8.7-injected" {
			t.Fatalf("got %q, want the injected version unchanged", got)
		}
	})

	t.Run("default never leaks devel sentinel", func(t *testing.T) {
		for _, v := range []string{"dev", ""} {
			version = v
			if got := effectiveVersion(); got == "(devel)" {
				t.Fatalf("version %q: got (devel) sentinel", v)
			}
		}
	})
}
