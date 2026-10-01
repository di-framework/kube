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

// fakeKubesoloctl behaves like kubesoloctl towards the invoking user's kubeconfig:
// install overwrites $HOME/.kube/config (KUBECONFIG and SUDO_USER would redirect
// it), and uninstall deletes it. It records the HOME each call saw.
const fakeKubesoloctl = `#!/bin/sh
set -e
echo "$1 HOME=$HOME KUBECONFIG=$KUBECONFIG SUDO_USER=$SUDO_USER" >> "$FAKE_STATE/calls"
case "$1" in
install)
  mkdir -p "$HOME/.kube"
  printf 'users:\n- name: kubernetes-admin\n  user: {token: %s}\n' "$FAKE_STATE" > "$HOME/.kube/config"
  touch "$FAKE_STATE/installed"
  ;;
uninstall)
  rm -f "$HOME/.kube/config"
  ;;
kubeconfig)
  [ -f "$FAKE_STATE/installed" ] || exit 1
  printf 'apiVersion: v1\nkind: Config\nclusters: []\n'
  ;;
esac
`

func TestKubesoloctlNeverTouchesTheSharedKubeconfig(t *testing.T) {
	home := t.TempDir()
	stateDir := t.TempDir()
	shared := filepath.Join(home, ".kube", "config")
	original := "users:\n- name: kubernetes-admin\n  user: {token: other-instance}\n"
	if err := os.MkdirAll(filepath.Dir(shared), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(shared, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(stateDir, "kubesoloctl")
	if err := os.WriteFile(binary, []byte(fakeKubesoloctl), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("KUBECONFIG", shared)
	t.Setenv("SUDO_USER", "someone")
	t.Setenv("FAKE_STATE", stateDir)

	manager := Manager{Binary: binary}
	options := Options{Name: "dev", RunMode: "service"}
	data, created, err := manager.Ensure(context.Background(), options)
	if err != nil {
		t.Fatalf("Ensure() error = %v", err)
	}
	if !created || !strings.Contains(string(data), "clusters:") {
		t.Fatalf("Ensure() = %q, %v; want the exported kubeconfig of a new instance", data, created)
	}
	if err := manager.Uninstall(context.Background(), options, true); err != nil {
		t.Fatalf("Uninstall() error = %v", err)
	}

	got, err := os.ReadFile(shared)
	if err != nil || string(got) != original {
		t.Fatalf("shared kubeconfig = %q, %v; want it untouched", got, err)
	}
	calls, err := os.ReadFile(filepath.Join(stateDir, "calls"))
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(strings.TrimSpace(string(calls)), "\n") {
		if !strings.HasPrefix(line, "install ") && !strings.HasPrefix(line, "uninstall ") {
			continue
		}
		fields := strings.Fields(line)
		private := strings.TrimPrefix(fields[1], "HOME=")
		if private == home || private == "" {
			t.Fatalf("%s ran with the user's HOME: %q", fields[0], line)
		}
		if fields[2] != "KUBECONFIG=" || fields[3] != "SUDO_USER=" {
			t.Fatalf("%s inherited the user's kubeconfig location: %q", fields[0], line)
		}
		if _, err := os.Stat(private); !os.IsNotExist(err) {
			t.Fatalf("%s left its private HOME %s behind (stat error %v)", fields[0], private, err)
		}
	}
}

func TestIsolatedEnv(t *testing.T) {
	t.Parallel()
	got := isolatedEnv([]string{
		"HOME=/Users/dev", "PATH=/bin", "KUBECONFIG=/Users/dev/.kube/config",
		"SUDO_USER=dev", "SUDO_UID=501", "SUDO_GID=20", "DOAS_USER=dev", "DOCKER_HOST=unix:///podman.sock",
	}, "/tmp/private")
	want := []string{"PATH=/bin", "DOCKER_HOST=unix:///podman.sock", "HOME=/tmp/private"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("isolatedEnv() = %#v, want %#v", got, want)
	}
}
