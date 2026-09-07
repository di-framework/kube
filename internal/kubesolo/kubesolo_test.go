package kubesolo

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestInstallArgsContainer(t *testing.T) {
	t.Parallel()
	want := []string{
		"install", "--name", "dev", "--version", "v1.2.0", "--run-mode", "container",
		"--container-ports", "127.0.0.1:28080:30080",
	}
	got := installArgs("v1.2.0", Options{Name: "dev", RunMode: "container", HTTPPort: 28080, NodePort: 30080})
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("installArgs() = %#v, want %#v", got, want)
	}
}

func TestInstallArgsService(t *testing.T) {
	t.Parallel()
	want := []string{"install", "--name", "edge", "--version", "v1.2.0", "--run-mode", "service", "--path", "/data/kubesolo"}
	got := installArgs("v1.2.0", Options{Name: "edge", RunMode: "service", DataPath: "/data/kubesolo"})
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("installArgs() = %#v, want %#v", got, want)
	}
}

func TestDownloadVerifiesAndCachesBinary(t *testing.T) {
	t.Parallel()
	payload := []byte("test kubesoloctl")
	sum := sha256.Sum256(payload)
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1.2.0/kubesoloctl-test-test" {
			t.Errorf("unexpected URL %s", request.URL.Path)
		}
		response.Write(payload)
	}))
	defer server.Close()

	manager := Manager{
		CacheDir:  t.TempDir(),
		BaseURL:   server.URL,
		GOOS:      "test",
		GOARCH:    "test",
		Checksums: map[string]string{"test/test": hex.EncodeToString(sum[:])},
	}
	path, err := manager.download(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != string(payload) {
		t.Fatalf("downloaded %q, want %q", data, payload)
	}
	if filepath.Base(path) != "kubesoloctl-test-test" {
		t.Fatalf("unexpected path %q", path)
	}

	server.Close()
	if _, err := manager.download(context.Background()); err != nil {
		t.Fatalf("cached download failed after server stopped: %v", err)
	}
}

func TestDownloadRejectsWrongChecksum(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Write([]byte("tampered"))
	}))
	defer server.Close()
	manager := Manager{
		CacheDir:  t.TempDir(),
		BaseURL:   server.URL,
		GOOS:      "test",
		GOARCH:    "test",
		Checksums: map[string]string{"test/test": string(make([]byte, 64))},
	}
	if _, err := manager.download(context.Background()); err == nil {
		t.Fatal("download unexpectedly accepted a wrong checksum")
	}
}

func TestPatchKubeconfigServer(t *testing.T) {
	t.Parallel()
	input := []byte(`apiVersion: v1
kind: Config
current-context: admin
clusters:
- name: kubesolo
  cluster:
    server: https://172.21.0.2:6443
contexts:
- name: admin
  context:
    cluster: kubesolo
    user: admin
users:
- name: admin
  user:
    token: test
`)
	patched, err := patchKubeconfigServer(input, "https://127.0.0.1:55467")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(patched), "server: https://127.0.0.1:55467") {
		t.Fatalf("server was not patched:\n%s", patched)
	}
}
