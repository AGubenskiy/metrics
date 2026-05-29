package buildinfo

import (
	"bytes"
	"errors"
	"testing"
)

func TestPrint(t *testing.T) {
	var buf bytes.Buffer

	if err := Print(&buf, "1.2.3", "2026-05-27", "abcdef"); err != nil {
		t.Fatalf("Print() error = %v", err)
	}

	want := "Build version: 1.2.3\nBuild date: 2026-05-27\nBuild commit: abcdef\n"
	if buf.String() != want {
		t.Fatalf("Print() output = %q, want %q", buf.String(), want)
	}
}

func TestPrintUsesProvidedValuesAsIs(t *testing.T) {
	var buf bytes.Buffer

	if err := Print(&buf, "N/A", "N/A", "N/A"); err != nil {
		t.Fatalf("Print() error = %v", err)
	}

	want := "Build version: N/A\nBuild date: N/A\nBuild commit: N/A\n"
	if buf.String() != want {
		t.Fatalf("Print() output = %q, want %q", buf.String(), want)
	}
}

func TestPrintPropagatesWriterError(t *testing.T) {
	wantErr := errors.New("write failed")

	err := Print(failingWriter{err: wantErr}, "1.2.3", "2026-05-27", "abcdef")
	if !errors.Is(err, wantErr) {
		t.Fatalf("Print() error = %v, want %v", err, wantErr)
	}
}

type failingWriter struct {
	err error
}

func (w failingWriter) Write(_ []byte) (int, error) {
	return 0, w.err
}
