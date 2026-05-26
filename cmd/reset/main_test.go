package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunGeneratesResetMethods(t *testing.T) {
	root := t.TempDir()

	writeTestFiles(t, root, map[string]string{
		"go.mod": "module example.com/resettest\n\ngo 1.26.0\n",
		filepath.Join("sample", "types.go"): `package sample

import "time"

type inner struct {
	N     int
	Items []string
	Dict  map[string]int
}

type manual struct {
	N int
}

func (m *manual) Reset() {
	m.N = 99
}

// generate:reset
type Parent struct {
	I         int
	S         string
	P         *string
	Items     []string
	Dict      map[string]int
	Inner     inner
	InnerPtr  *inner
	Manual    manual
	ManualPtr *manual
	When      time.Time
}
`,
		filepath.Join("sample", "types_test.go"): `package sample

import (
	"testing"
	"time"
)

func TestParentReset(t *testing.T) {
	pointerValue := "value"
	parent := &Parent{
		I:     42,
		S:     "hello",
		P:     &pointerValue,
		Items: []string{"a", "b"},
		Dict:  map[string]int{"x": 1},
		Inner: inner{
			N:     7,
			Items: []string{"c"},
			Dict:  map[string]int{"y": 2},
		},
		InnerPtr: &inner{
			N:     9,
			Items: []string{"d"},
			Dict:  map[string]int{"z": 3},
		},
		Manual:    manual{N: 1},
		ManualPtr: &manual{N: 2},
		When:      time.Now(),
	}

	parent.Reset()

	if parent.I != 0 || parent.S != "" {
		t.Fatalf("primitive fields were not reset: %+v", parent)
	}
	if parent.P == nil || *parent.P != "" {
		t.Fatalf("pointer field was not reset: %#v", parent.P)
	}
	if parent.Items == nil || len(parent.Items) != 0 {
		t.Fatalf("slice field was not trimmed: %#v", parent.Items)
	}
	if parent.Dict == nil || len(parent.Dict) != 0 {
		t.Fatalf("map field was not cleared: %#v", parent.Dict)
	}
	if parent.Inner.N != 0 || parent.Inner.Items == nil || len(parent.Inner.Items) != 0 || parent.Inner.Dict == nil || len(parent.Inner.Dict) != 0 {
		t.Fatalf("nested struct was not reset: %+v", parent.Inner)
	}
	if parent.InnerPtr == nil || parent.InnerPtr.N != 0 || parent.InnerPtr.Items == nil || len(parent.InnerPtr.Items) != 0 || parent.InnerPtr.Dict == nil || len(parent.InnerPtr.Dict) != 0 {
		t.Fatalf("nested pointer struct was not reset: %+v", parent.InnerPtr)
	}
	if parent.Manual.N != 99 || parent.ManualPtr == nil || parent.ManualPtr.N != 99 {
		t.Fatalf("custom Reset was not used: %+v / %+v", parent.Manual, parent.ManualPtr)
	}
	if !parent.When.IsZero() {
		t.Fatalf("imported struct was not zeroed: %+v", parent.When)
	}
}
`,
	})

	if err := run(root); err != nil {
		t.Fatalf("run() error = %v", err)
	}

	generatedPath := filepath.Join(root, "sample", generatedFileName)
	data, err := os.ReadFile(generatedPath)
	if err != nil {
		t.Fatalf("read generated file: %v", err)
	}

	generated := string(data)
	if !strings.Contains(generated, "func (r *Parent) Reset()") {
		t.Fatalf("generated file does not contain Reset method:\n%s", generated)
	}
	if !strings.Contains(generated, "r.Items = r.Items[:0]") {
		t.Fatalf("generated file does not trim slices:\n%s", generated)
	}
	if !strings.Contains(generated, "clear(r.Dict)") {
		t.Fatalf("generated file does not clear maps:\n%s", generated)
	}

	cmd := exec.Command("go", "test", "./...")
	cmd.Dir = root
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go test ./... failed: %v\n%s", err, output)
	}
}

func TestRunFailsOnManualResetConflict(t *testing.T) {
	root := t.TempDir()

	writeTestFiles(t, root, map[string]string{
		"go.mod": "module example.com/resetconflict\n\ngo 1.26.0\n",
		filepath.Join("sample", "types.go"): `package sample

// generate:reset
type Conflict struct {
	N int
}

func (c *Conflict) Reset() {}
`,
	})

	err := run(root)
	if err == nil {
		t.Fatal("run() expected conflict error, got nil")
	}
	if !strings.Contains(err.Error(), "already declares Reset") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func writeTestFiles(t *testing.T, root string, files map[string]string) {
	t.Helper()

	for relativePath, content := range files {
		path := filepath.Join(root, relativePath)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", path, err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}
}
