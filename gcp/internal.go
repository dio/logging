package gcp

import (
	"io"
	"os"
)

// stderrForOverflow is the writer used by the labels handler's
// one-time overflow warning. Test seam; not part of the public API.
var stderrForOverflow io.Writer = os.Stderr
