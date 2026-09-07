package cluster

import (
	"os"
	"path/filepath"
	"testing"
)

func TestServerUsesSelectedContext(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "config")
	data := []byte(`apiVersion: v1
kind: Config
current-context: first
clusters:
- name: one
  cluster:
    server: https://one.example
- name: two
  cluster:
    server: https://two.example
contexts:
- name: first
  context:
    cluster: one
    user: admin
- name: second
  context:
    cluster: two
    user: admin
users:
- name: admin
  user:
    token: test
`)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := Server(path, "second")
	if err != nil {
		t.Fatal(err)
	}
	if got != "https://two.example" {
		t.Fatalf("Server() = %q", got)
	}
}
