# Contributing to kbcli

Thanks for your interest in improving kbcli. This guide covers the local
workflow and the conventions the project follows. See [README.md](README.md)
for an overview of the project.

## Prerequisites

- Go 1.27.4.
- No CGO toolchain is required; the default build is pure Go.
- No server or external service is needed to build, test, or run kbcli.

## Development

Common tasks are available through the Makefile:

- `make build` - compile the binaries.
- `make test` - run the test suite.
- `make race` - run the test suite with the race detector.
- `make vet` - run `go vet`.
- `make fmt` - format the source with gofmt.

## Code conventions

- Write all comments in English.
- Write all commit messages in English.
- Format all Go code with gofmt before committing.
- Chinese UI copy belongs only in `internal/i18n/strings_zh.go`.
- Chinese fact markers and Chinese test fixtures are intentional and must not
  be translated or removed.

## Testing

- Add tests for every new behavior or bug fix.
- Run `go test -race ./...` before opening a pull request.

## Cross-compilation

The tree is designed to cross-compile without CGO:

```
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build ./...
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build ./...
```

Note: the Windows runtime has NOT been verified on real hardware.

## Pull request process

- Describe the change clearly.
- Link the relevant issue.
- Keep the test suite green (`go test -race ./...`).
