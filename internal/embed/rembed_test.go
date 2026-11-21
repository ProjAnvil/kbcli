package embed

// A rembed model must be downloaded to exercise CountForEmbedding or MaxTokens
// at runtime, so no runtime test is added here (tests must not download a
// model). The compile-time assertion below pins the interface contract; the
// methods themselves are thin wrappers around rembed.Tokenize and the cached
// probe in newRembed.
var _ TokenCounter = (*rembedEmbedder)(nil)
