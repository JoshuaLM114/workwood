package fileio

import (
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestWrite(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "raw.txt")
	if err := Write(p, []byte("hello"), 0o644); err != nil {
		t.Fatalf("Write: %v", err)
	}
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(b) != "hello" {
		t.Fatalf("content = %q, want %q", b, "hello")
	}
}

func TestWriteReplacesAtomically(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "f")
	if err := Write(p, []byte("first"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Write(p, []byte("second"), 0o644); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(p); string(b) != "second" {
		t.Fatalf("after replace = %q, want second", b)
	}
	// The temp file must have been renamed away, not left behind.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "f" {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Fatalf("dir holds %v, want only [f]", names)
	}
}

func TestWriteYAMLRoundTrips(t *testing.T) {
	type doc struct {
		Name  string   `yaml:"name"`
		Items []string `yaml:"items"`
	}
	in := doc{Name: "x", Items: []string{"a", "b"}}

	p := filepath.Join(t.TempDir(), "doc.yaml")
	if err := WriteYAML(p, in); err != nil {
		t.Fatalf("WriteYAML: %v", err)
	}
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	var got doc
	if err := yaml.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, b)
	}
	if got.Name != in.Name || len(got.Items) != 2 || got.Items[0] != "a" || got.Items[1] != "b" {
		t.Fatalf("round-trip = %+v, want %+v", got, in)
	}
}
