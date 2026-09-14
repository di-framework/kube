package platform

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"sigs.k8s.io/yaml"
)

const DefaultPlatformPackage = "@di-framework/platform@5.3.3"

// The wrapper contains no infrastructure definitions: both entrypoints use this package.
const platformProgram = `export { schemaVersion, kubeconfig, namespace, endpoints, tenants, users } from '@di-framework/platform/existing';
`

type Pulumi struct {
	Debug               bool
	StorageRoot         string
	NetworkPolicyEngine string
	OnClaim             func() error
	Directory           string
	Package             string
	Kubeconfig          string
	Context             string
	Namespace           string
	ConfigFile          string
	HTTPPort            int
	Server              string
	Stdout              io.Writer
	Stderr              io.Writer
	ownership           func(context.Context, projectIdentity, bool) error
	// run is injectable so lifecycle tests do not provision real clusters.
	run func(context.Context, string, []string, string, []string) ([]byte, error)
}

type projectIdentity struct {
	Kubeconfig string `json:"kubeconfig"`
	Context    string `json:"context"`
	Namespace  string `json:"namespace"`
	Release    string `json:"release"`
	Server     string `json:"server"`
	Owner      string `json:"owner"`
	Package    string `json:"package"`
}

func (p Pulumi) command(ctx context.Context, name string, args ...string) ([]byte, error) {
	if name == "pulumi" && p.Debug {
		args = append(args, "--logtostderr", "--verbose=3")
	}
	passphrase, err := os.ReadFile(filepath.Join(p.Directory, ".passphrase"))
	if err != nil {
		return nil, fmt.Errorf("read Pulumi secrets passphrase: %w", err)
	}
	env := append(os.Environ(), "PULUMI_CONFIG_PASSPHRASE="+string(passphrase), "PULUMI_BACKEND_URL="+p.backend())
	if p.run != nil {
		return p.run(ctx, name, args, p.Directory, env)
	}
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = p.Directory
	cmd.Env = env
	cmd.Stderr = writerOrDiscard(p.Stderr)
	var output bytes.Buffer
	cmd.Stdout = io.MultiWriter(&output, writerOrDiscard(p.Stdout))
	err = cmd.Run()
	if err != nil {
		return nil, fmt.Errorf("%s %s failed: %w", name, strings.Join(args, " "), err)
	}
	return output.Bytes(), nil
}

func (p Pulumi) backend() string {
	return (&url.URL{Scheme: "file", Path: filepath.Join(p.Directory, "state")}).String()
}

func (p Pulumi) prepare(options InstallOptions) (projectIdentity, error) {
	if !filepath.IsAbs(p.Directory) {
		return projectIdentity{}, errors.New("Pulumi project directory must be absolute")
	}
	if err := os.MkdirAll(p.Directory, 0700); err != nil {
		return projectIdentity{}, err
	}
	if err := os.MkdirAll(filepath.Join(p.Directory, "state"), 0700); err != nil {
		return projectIdentity{}, err
	}
	pkg := p.Package
	if pkg == "" {
		pkg = DefaultPlatformPackage
	}
	identity := projectIdentity{Kubeconfig: p.Kubeconfig, Context: p.Context, Namespace: p.Namespace, Release: options.Release, Server: p.Server, Package: pkg}
	identityPath := filepath.Join(p.Directory, "identity.json")
	data, err := os.ReadFile(identityPath)
	if err == nil {
		var saved projectIdentity
		if err := json.Unmarshal(data, &saved); err != nil {
			return identity, err
		}
		identity.Owner = saved.Owner
		// Explicit package upgrades are allowed, changing the target is not.
		saved.Package = pkg
		if saved != identity {
			return identity, errors.New("this Pulumi project already owns a different cluster, namespace, or release; use its original settings")
		}
	} else if errors.Is(err, os.ErrNotExist) {
		secret := make([]byte, 32)
		if _, err := rand.Read(secret); err != nil {
			return identity, err
		}
		identity.Owner = hex.EncodeToString(secret)
		if _, err := rand.Read(secret); err != nil {
			return identity, err
		}
		if err := os.WriteFile(filepath.Join(p.Directory, ".passphrase"), []byte(hex.EncodeToString(secret)), 0600); err != nil {
			return identity, err
		}
	} else {
		return identity, err
	}
	spec := strings.TrimPrefix(pkg, "@di-framework/platform@")
	if strings.HasPrefix(pkg, "file:") {
		spec = pkg
	}
	if (!strings.HasPrefix(pkg, "file:") && (spec == pkg || !regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+(?:-[A-Za-z0-9.-]+)?$`).MatchString(spec))) || (strings.HasPrefix(pkg, "file:") && !filepath.IsAbs(strings.TrimPrefix(pkg, "file:"))) {
		return identity, errors.New("platform package must be @di-framework/platform@<version> or file:<absolute tarball path>")
	}
	config := map[string]any{}
	// Preserve declarations when an update omits --platform-config.
	if old, err := os.ReadFile(filepath.Join(p.Directory, "Pulumi.dev.yaml")); err == nil {
		var stack struct {
			Config map[string]any `json:"config"`
		}
		if err := yaml.Unmarshal(old, &stack); err != nil {
			return identity, err
		}
		for _, key := range []string{"tenants", "users", "tenantHostImage", "tenantHostImagePullPolicy", "networkPolicyEngine", "storageRoot"} {
			if value, ok := stack.Config["di-framework-kube:"+key]; ok {
				config[key] = value
			}
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return identity, err
	}
	if p.ConfigFile != "" {
		b, err := os.ReadFile(p.ConfigFile)
		if err != nil {
			return identity, err
		}
		config = map[string]any{}
		if err := json.Unmarshal(b, &config); err != nil {
			return identity, fmt.Errorf("decode platform config JSON: %w", err)
		}
		if config == nil {
			return identity, errors.New("platform config must be a JSON object")
		}
		for k := range config {
			if k != "tenants" && k != "users" && k != "tenantHostImage" && k != "tenantHostImagePullPolicy" && k != "networkPolicyEngine" && k != "storageRoot" {
				return identity, fmt.Errorf("unsupported platform config key %q", k)
			}
		}
	}
	values := map[string]any{}
	for _, file := range options.ValueFiles {
		b, err := os.ReadFile(file)
		if err != nil {
			return identity, err
		}
		var value map[string]any
		if err := yaml.Unmarshal(b, &value); err != nil {
			return identity, err
		}
		values = merge(values, value)
	}
	if _, ok := config["networkPolicyEngine"]; !ok {
		engine := p.NetworkPolicyEngine
		if engine == "" {
			engine = "existing"
		}
		config["networkPolicyEngine"] = engine
	}
	if _, ok := config["storageRoot"]; !ok && p.StorageRoot != "" {
		config["storageRoot"] = p.StorageRoot
	}
	config["kubeconfig"] = p.Kubeconfig
	config["context"] = p.Context
	config["namespace"] = p.Namespace
	config["release"] = options.Release
	config["chart"] = options.Chart
	config["chartVersion"] = options.ChartVersion
	config["httpNodePort"] = options.NodePort
	config["timeoutSeconds"] = int(options.Timeout.Seconds())
	config["insecureRegistry"] = options.AllowInsecureRegistries
	config["values"] = values
	config["kubernetesEndpoint"] = p.Server
	if p.HTTPPort > 0 {
		config["httpEndpoint"] = fmt.Sprintf("http://127.0.0.1:%d", p.HTTPPort)
	}
	namespaced := map[string]any{}
	for key, value := range config {
		namespaced["di-framework-kube:"+key] = value
	}
	stackConfig := map[string]any{}
	if b, err := os.ReadFile(filepath.Join(p.Directory, "Pulumi.dev.yaml")); err == nil {
		if err := yaml.Unmarshal(b, &stackConfig); err != nil {
			return identity, err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return identity, err
	}
	stackConfig["config"] = namespaced
	files := map[string]any{
		"identity.json":   identity,
		"package.json":    map[string]any{"private": true, "main": "index.ts", "dependencies": map[string]string{"@di-framework/platform": spec, "typescript": "5.9.3", "@types/node": "22.18.0"}},
		"Pulumi.yaml":     map[string]any{"name": "di-framework-kube", "runtime": map[string]any{"name": "nodejs", "options": map[string]any{"typescript": true}}, "backend": map[string]string{"url": p.backend()}},
		"Pulumi.dev.yaml": stackConfig,
		"tsconfig.json":   map[string]any{"compilerOptions": map[string]any{"target": "ES2022", "module": "commonjs", "moduleResolution": "node", "strict": true, "skipLibCheck": true, "esModuleInterop": true}, "include": []string{"index.ts"}},
	}
	for name, value := range files {
		b, err := json.MarshalIndent(value, "", "  ")
		if err != nil {
			return identity, err
		}
		if err := os.WriteFile(filepath.Join(p.Directory, name), append(b, '\n'), 0600); err != nil {
			return identity, err
		}
	}
	if err := os.WriteFile(filepath.Join(p.Directory, "index.ts"), []byte(platformProgram), 0600); err != nil {
		return identity, err
	}
	return identity, nil
}

// CheckPrerequisites validates provisioning tools before callers create infrastructure.
func CheckPrerequisites() error {
	return checkExecutables("pulumi", "npm", "node")
}

func checkExecutables(names ...string) error {
	for _, name := range names {
		if _, err := exec.LookPath(name); err != nil {
			return fmt.Errorf("shared platform requires %s on PATH: %w", name, err)
		}
	}
	return nil
}

func (p Pulumi) Up(ctx context.Context, options InstallOptions) error {
	// Fail before claiming a cluster if the required executables are absent.
	if p.run == nil {
		if err := CheckPrerequisites(); err != nil {
			return err
		}
	}
	identity, err := p.prepare(options)
	if err != nil {
		return err
	}
	if err := p.claim(ctx, identity, false); err != nil {
		return err
	}
	if p.OnClaim != nil {
		if err := p.OnClaim(); err != nil {
			return err
		}
	}
	for _, args := range [][]string{{"install", "--no-audit", "--no-fund"}} {
		_, err := p.command(ctx, "npm", args...)
		if err != nil {
			return err
		}

	}
	if _, err := p.command(ctx, "pulumi", "stack", "select", "dev", "--create", "--non-interactive"); err != nil {
		return err
	}
	_, err = p.command(ctx, "pulumi", "up", "--stack", "dev", "--yes", "--non-interactive")

	return err
}

func (p Pulumi) Destroy(ctx context.Context, timeout time.Duration) error {
	// Destroy evaluates the Node.js program but does not install npm packages.
	// Check before reading state or making any ownership changes.
	if p.run == nil {
		if err := checkExecutables("pulumi", "node"); err != nil {
			return err
		}
	}
	data, err := os.ReadFile(filepath.Join(p.Directory, "identity.json"))
	if err != nil {
		return err
	}
	var identity projectIdentity
	if err := json.Unmarshal(data, &identity); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	if err := p.claim(ctx, identity, false); err != nil {
		return err
	}
	_, err = p.command(ctx, "pulumi", "destroy", "--stack", "dev", "--yes", "--non-interactive")

	if err != nil {
		return err
	}
	return p.claim(ctx, identity, true)
}
