package kubesolo

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"
	"k8s.io/client-go/tools/clientcmd"
)

const (
	DefaultVersion = "v1.2.0"
	releaseBaseURL = "https://github.com/portainer/kubesolo/releases/download"
)

var defaultChecksums = map[string]string{
	"darwin/amd64": "4bdc6b40bdf6493b9776827d39a7961040adc60448023dce1e38863e76cf9e14",
	"darwin/arm64": "9c98e6a5a284087dbf22b9288347d21d658cfa09bf62b5ea20849f592efc0987",
	"linux/amd64":  "8292e0a57224dc6f5206f005040cc40aa26f8b775f67d35e11882aef7f1ca116",
	"linux/arm64":  "25ebc4c920a4dfe9cd85bb3daa90080d68bbe385b0467f103af8f934d20a3957",
}

type Options struct {
	Name       string
	RunMode    string
	DataPath   string
	HTTPPort   int
	NodePort   int
	Kubeconfig string
}

type Manager struct {
	Version    string
	Binary     string
	CacheDir   string
	Stdout     io.Writer
	Stderr     io.Writer
	HTTPClient *http.Client
	BaseURL    string
	GOOS       string
	GOARCH     string
	Checksums  map[string]string
	Probe      func([]byte) error
}

func (m *Manager) Ensure(ctx context.Context, options Options) ([]byte, bool, error) {
	binary, err := m.resolveBinary(ctx)
	if err != nil {
		return nil, false, err
	}

	if data, err := m.exportKubeconfig(ctx, binary, options); err == nil {
		if m.Probe == nil || m.Probe(data) == nil {
			return data, false, nil
		}
	}

	cmd := exec.CommandContext(ctx, binary, installArgs(m.version(), options)...)
	cmd.Stdout = writerOrDiscard(m.Stdout)
	cmd.Stderr = writerOrDiscard(m.Stderr)
	if err := cmd.Run(); err != nil {
		return nil, false, fmt.Errorf("install Kubesolo: %w", err)
	}

	deadline := time.NewTimer(2 * time.Minute)
	defer deadline.Stop()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	var lastErr error
	for {
		data, exportErr := m.exportKubeconfig(ctx, binary, options)
		if exportErr == nil {
			return data, true, nil
		}
		lastErr = exportErr
		select {
		case <-ctx.Done():
			return nil, true, ctx.Err()
		case <-deadline.C:
			return nil, true, fmt.Errorf("Kubesolo did not publish a kubeconfig: %w", lastErr)
		case <-ticker.C:
		}
	}
}

func (m *Manager) Uninstall(ctx context.Context, options Options, purge bool) error {
	binary, err := m.resolveBinary(ctx)
	if err != nil {
		return err
	}
	args := []string{"uninstall", "--name", options.Name, "--remove-kubeconfig"}
	if purge {
		args = append(args, "--purge")
	}
	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.Stdout = writerOrDiscard(m.Stdout)
	cmd.Stderr = writerOrDiscard(m.Stderr)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("uninstall Kubesolo: %w", err)
	}
	return nil
}

func (m *Manager) exportKubeconfig(ctx context.Context, binary string, options Options) ([]byte, error) {
	args := []string{"kubeconfig", "view"}
	if options.RunMode == "container" {
		args = append(args, "--container", ContainerName(options.Name))
	} else if options.DataPath != "" {
		args = append(args, "--path", options.DataPath)
	}
	cmd := exec.CommandContext(ctx, binary, args...)
	data, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("export Kubesolo kubeconfig: %w", err)
	}
	if !strings.Contains(string(data), "apiVersion:") || !strings.Contains(string(data), "clusters:") {
		return nil, errors.New("Kubesolo returned an invalid kubeconfig")
	}
	if options.RunMode == "container" {
		server, err := containerAPIServer(ctx, ContainerName(options.Name))
		if err != nil {
			return nil, fmt.Errorf("discover Kubesolo API endpoint: %w", err)
		}
		data, err = patchKubeconfigServer(data, server)
		if err != nil {
			return nil, err
		}
	}
	return data, nil
}

func containerAPIServer(ctx context.Context, containerName string) (string, error) {
	docker, err := client.New(client.FromEnv)
	if err != nil {
		return "", fmt.Errorf("create container engine client: %w", err)
	}
	defer docker.Close()
	result, err := docker.ContainerInspect(ctx, containerName, client.ContainerInspectOptions{})
	if err != nil {
		return "", fmt.Errorf("inspect container %q: %w", containerName, err)
	}
	if result.Container.NetworkSettings == nil {
		return "", fmt.Errorf("container %q has no network settings", containerName)
	}
	bindings := result.Container.NetworkSettings.Ports[network.MustParsePort("6443/tcp")]
	if len(bindings) == 0 || bindings[0].HostPort == "" {
		return "", fmt.Errorf("container %q has no host binding for 6443/tcp", containerName)
	}
	port, err := strconv.Atoi(bindings[0].HostPort)
	if err != nil || port < 1 || port > 65535 {
		return "", fmt.Errorf("container %q has invalid API port %q", containerName, bindings[0].HostPort)
	}
	return fmt.Sprintf("https://127.0.0.1:%d", port), nil
}

func patchKubeconfigServer(data []byte, server string) ([]byte, error) {
	config, err := clientcmd.Load(data)
	if err != nil {
		return nil, fmt.Errorf("decode Kubesolo kubeconfig: %w", err)
	}
	contextConfig, ok := config.Contexts[config.CurrentContext]
	if !ok {
		return nil, fmt.Errorf("Kubesolo kubeconfig current context %q does not exist", config.CurrentContext)
	}
	clusterConfig, ok := config.Clusters[contextConfig.Cluster]
	if !ok {
		return nil, fmt.Errorf("Kubesolo kubeconfig cluster %q does not exist", contextConfig.Cluster)
	}
	clusterConfig.Server = server
	patched, err := clientcmd.Write(*config)
	if err != nil {
		return nil, fmt.Errorf("encode Kubesolo kubeconfig: %w", err)
	}
	return patched, nil
}

func installArgs(version string, options Options) []string {
	args := []string{
		"install",
		"--name", options.Name,
		"--version", version,
		"--run-mode", options.RunMode,
	}
	if options.DataPath != "" && options.RunMode != "container" {
		args = append(args, "--path", options.DataPath)
	}
	if options.RunMode == "container" && options.HTTPPort > 0 {
		args = append(args, "--container-ports", fmt.Sprintf("127.0.0.1:%d:%d", options.HTTPPort, options.NodePort))
	}
	return args
}

func ContainerName(name string) string {
	if name == "" || name == "kubesolo" {
		return "kubesolo"
	}
	return "kubesolo-" + name
}

func (m *Manager) resolveBinary(ctx context.Context) (string, error) {
	if m.Binary != "" {
		path, err := exec.LookPath(m.Binary)
		if err != nil {
			return "", fmt.Errorf("find kubesoloctl %q: %w", m.Binary, err)
		}
		return path, nil
	}
	if path, err := exec.LookPath("kubesoloctl"); err == nil {
		return path, nil
	}
	if m.version() != DefaultVersion {
		return "", fmt.Errorf("automatic kubesoloctl download supports pinned version %s; supply --kubesoloctl for %s", DefaultVersion, m.version())
	}
	return m.download(ctx)
}

func (m *Manager) download(ctx context.Context) (string, error) {
	goos, goarch := m.platform()
	key := goos + "/" + goarch
	checksums := m.Checksums
	if checksums == nil {
		checksums = defaultChecksums
	}
	want, ok := checksums[key]
	if !ok {
		return "", fmt.Errorf("automatic kubesoloctl download is not available for %s; supply --kubesoloctl", key)
	}
	cacheDir := m.CacheDir
	if cacheDir == "" {
		base, err := os.UserCacheDir()
		if err != nil {
			return "", fmt.Errorf("find user cache directory: %w", err)
		}
		cacheDir = filepath.Join(base, "di-framework-kube", "bin")
	}
	name := fmt.Sprintf("kubesoloctl-%s-%s", goos, goarch)
	path := filepath.Join(cacheDir, m.version(), name)
	if checksumMatches(path, want) {
		return path, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", fmt.Errorf("create binary cache: %w", err)
	}

	baseURL := strings.TrimRight(m.BaseURL, "/")
	if baseURL == "" {
		baseURL = releaseBaseURL
	}
	url := fmt.Sprintf("%s/%s/%s", baseURL, m.version(), name)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("create kubesoloctl request: %w", err)
	}
	client := m.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Minute}
	}
	response, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("download kubesoloctl: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download kubesoloctl: %s", response.Status)
	}

	tmp, err := os.CreateTemp(filepath.Dir(path), ".kubesoloctl-*")
	if err != nil {
		return "", fmt.Errorf("create temporary binary: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	hash := sha256.New()
	if _, err := io.Copy(io.MultiWriter(tmp, hash), response.Body); err != nil {
		tmp.Close()
		return "", fmt.Errorf("write kubesoloctl: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return "", fmt.Errorf("close kubesoloctl: %w", err)
	}
	got := hex.EncodeToString(hash.Sum(nil))
	if got != want {
		return "", fmt.Errorf("verify kubesoloctl: sha256 %s, want %s", got, want)
	}
	if err := os.Chmod(tmpPath, 0o755); err != nil {
		return "", fmt.Errorf("make kubesoloctl executable: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return "", fmt.Errorf("install cached kubesoloctl: %w", err)
	}
	return path, nil
}

func (m *Manager) version() string {
	if m.Version == "" {
		return DefaultVersion
	}
	return m.Version
}

func (m *Manager) platform() (string, string) {
	goos, goarch := m.GOOS, m.GOARCH
	if goos == "" {
		goos = runtime.GOOS
	}
	if goarch == "" {
		goarch = runtime.GOARCH
	}
	return goos, goarch
}

func checksumMatches(path, want string) bool {
	file, err := os.Open(path)
	if err != nil {
		return false
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return false
	}
	return hex.EncodeToString(hash.Sum(nil)) == want
}

func writerOrDiscard(writer io.Writer) io.Writer {
	if writer == nil {
		return io.Discard
	}
	return writer
}
