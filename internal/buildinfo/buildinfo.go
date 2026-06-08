package buildinfo

import (
	"fmt"
	"io"
)

// Print writes build metadata in the required startup format.
func Print(w io.Writer, version, date, commit string) error {
	_, err := fmt.Fprintf(
		w,
		"Build version: %s\nBuild date: %s\nBuild commit: %s\n",
		version,
		date,
		commit,
	)

	return err
}
