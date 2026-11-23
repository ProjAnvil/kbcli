package cli

import "errors"

// ExitError carries a stable process exit code for headless callers.
type ExitError struct {
	Code int
	Err  error
}

func (e *ExitError) Error() string { return e.Err.Error() }
func (e *ExitError) Unwrap() error { return e.Err }

func exitError(code int, err error) error {
	return &ExitError{Code: code, Err: err}
}

// ExitCode maps an error to the CLI contract:
// 0 success, 2 config, 3 no index, 4 LLM, 5 retrieval, 1 other.
func ExitCode(err error) int {
	if err == nil {
		return 0
	}
	var ee *ExitError
	if errors.As(err, &ee) {
		return ee.Code
	}
	if errors.Is(err, errNoIndex) {
		return 3
	}
	return 1
}
