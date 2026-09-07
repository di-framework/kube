package state

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStoreRoundTrip(t *testing.T) {
	t.Parallel()
	store := Store{Dir: t.TempDir()}
	want := State{
		Name:           "local-dev",
		ManagedCluster: true,
		Kubeconfig:     "/tmp/kubeconfig",
		Namespace:      "wasmcloud",
		Release:        "wasmcloud",
		ChartVersion:   "2.8.0",
	}
	if err := store.Save(want); err != nil {
		t.Fatal(err)
	}
	got, err := store.Load(want.Name)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != want.Name || got.ChartVersion != want.ChartVersion || got.UpdatedAt.IsZero() {
		t.Fatalf("unexpected state: %#v", got)
	}
}

func TestSaveKubeconfigUsesPrivatePermissions(t *testing.T) {
	t.Parallel()
	store := Store{Dir: t.TempDir()}
	path, err := store.SaveKubeconfig("dev", []byte("apiVersion: v1\n"))
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("mode = %o, want 600", got)
	}
	if filepath.Base(path) != "kubeconfig" {
		t.Fatalf("unexpected path %q", path)
	}
}

func TestValidateName(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"dev", "local-1", "a"} {
		if err := ValidateName(name); err != nil {
			t.Errorf("ValidateName(%q): %v", name, err)
		}
	}
	for _, name := range []string{"", "Upper", "-bad", "bad_underscore", "bad-"} {
		if err := ValidateName(name); err == nil {
			t.Errorf("ValidateName(%q) unexpectedly succeeded", name)
		}
	}
}
