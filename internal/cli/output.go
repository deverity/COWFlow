package cli

import (
	"encoding/json"
	"fmt"
	"io"
)

// IOStreams are the standard streams a command reads and writes.
type IOStreams struct {
	In       io.Reader
	Out, Err io.Writer
}

// printResult writes v as exactly one JSON document on stdout when asJSON
// is set, and calls human otherwise.
func printResult(ios IOStreams, asJSON bool, v any, human func(io.Writer) error) error {
	if asJSON {
		enc := json.NewEncoder(ios.Out)
		enc.SetIndent("", "  ")
		return enc.Encode(v)
	}
	return human(ios.Out)
}

type errorDoc struct {
	Error errorBody `json:"error"`
}

type errorBody struct {
	Message  string `json:"message"`
	ExitCode int    `json:"exit_code"`
}

// renderError writes err to stderr, as JSON in --json mode so stdout stays
// parseable.
func renderError(ios IOStreams, asJSON bool, code int, err error) {
	if asJSON {
		enc := json.NewEncoder(ios.Err)
		if encErr := enc.Encode(errorDoc{Error: errorBody{Message: err.Error(), ExitCode: code}}); encErr == nil {
			return
		}
	}
	// Nothing useful can be done if stderr itself fails.
	_, _ = fmt.Fprintf(ios.Err, "cowflow: %s\n", err)
}
