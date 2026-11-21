package embed

import (
	"fmt"
	"strings"
)

// newForProvider dispatches a provider name to its constructor. modelRef is
// ignored by the local provider.
func newForProvider(provider, modelRef string) (Embedder, error) {
	switch provider {
	case ProviderLocal:
		return newLocal(), nil
	case ProviderRembed:
		return newRembed(resolveRembedRef(modelRef))
	default:
		return nil, fmt.Errorf("embed: unknown provider %q (valid providers: %q, %q)",
			provider, ProviderLocal, ProviderRembed)
	}
}

// resolveRembedRef trims the model reference and falls back to the default
// when it is empty. rembed.Load performs the remaining validation of the ref
// shape, so no duplicated validation lives here.
func resolveRembedRef(modelRef string) string {
	ref := strings.TrimSpace(modelRef)
	if ref == "" {
		return DefaultRembedRef
	}
	return ref
}
