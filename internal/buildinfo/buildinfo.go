package buildinfo

import (
	"fmt"
	"io"
)

const notAvailable = "N/A"

// Print writes build metadata in the required startup format.
func Print(w io.Writer, version, date, commit string) error {
	_, err := fmt.Fprintf(
		w,
		"Build version: %s\nBuild date: %s\nBuild commit: %s\n",
		valueOrNA(version),
		valueOrNA(date),
		valueOrNA(commit),
	)

	return err
}

func valueOrNA(value string) string {
	if value == "" {
		return notAvailable
	}

	return value
}
