package platform

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
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
	"slices"
	"strings"
	"time"
	"unicode"

	"sigs.k8s.io/yaml"
)

// DefaultPlatformPackage is the shared platform release up installs. Override it with
// --platform-package @di-framework/platform@<version> or file:<absolute tarball path>.
const DefaultPlatformPackage = "@di-framework/platform@6.0.2"

// DefaultTenantHostImage is the published wash image with wasi-tls. The tag moves
// on the next publish; this digest is the multi-arch index. A platform-config
// tenantHostImage replaces it.
const DefaultTenantHostImage = "ghcr.io/di-framework/wash:2.8.0-wasi-tls@sha256:ee89fd4bce4f9f35f4cd09c63d3cbdd07bea3071b5d372f82c9f49b9741c3669"

// previousTenantHostImages are values this CLI used to write as
// DefaultTenantHostImage. A saved copy is the old default, not a user choice,
// so the next up replaces it. Add the previous constant here when the pin changes.
var previousTenantHostImages []string

func applyTenantHostImage(config map[string]any) {
	image, _ := config["tenantHostImage"].(string)
	if image == "" || image == DefaultTenantHostImage || slices.Contains(previousTenantHostImages, image) {
		config["tenantHostImage"] = DefaultTenantHostImage
	}
}

// The wrapper contains no infrastructure definitions: both entrypoints use this package.
// platformConfigKeys are the --platform-config keys passed to the platform's Pulumi config.
// An update without --platform-config keeps their previous values.
var platformConfigKeys = []string{
	"tenants",
	"users",
	"tenantHostImage",
	"tenantHostImagePullPolicy",
	"networkPolicyEngine",
	"storageRoot",
	"egressAllowedDestinations",
	"routeAliases",
	"tenantAuth",
	"insecureRegistries",
	"backingServiceClasses",
	"seedDefaultBackingClasses",
}

// keptValidatedKeys are checked again when an up without --platform-config keeps
// them, because they are also set by hand with pulumi config set.
var keptValidatedKeys = []string{"routeAliases", "insecureRegistries"}

// insecureRegistryRulesNote ends every insecureRegistries error. Only the shape is
// checked here: the platform owns the host[:port] grammar.
const insecureRegistryRulesNote = "the exact host[:port] grammar is checked by the platform during pulumi up"

// routeAliasRulesNote ends every routeAliases error. Only the shape is checked here:
// the platform owns the route grammar and checks it during pulumi up.
const routeAliasRulesNote = "route grammar, reserved names and alias chains are checked by the platform during pulumi up"

// validatePlatformConfigValue checks the shape of object-valued keys the platform
// reads with config.getObject. Values pass through to the stack config unchanged.
func validatePlatformConfigValue(key string, value any) error {
	switch key {
	case "routeAliases":
		aliases, ok := value.(map[string]any)
		if !ok {
			return fmt.Errorf("platform config routeAliases must be a JSON object mapping route names to route names, got %s (%s)", jsonKind(value), routeAliasRulesNote)
		}
		for name, target := range aliases {
			if name == "" {
				return fmt.Errorf("platform config routeAliases has an empty route name (%s)", routeAliasRulesNote)
			}
			if strings.IndexFunc(name, unicode.IsSpace) >= 0 {
				return fmt.Errorf("platform config routeAliases route name %q must not contain whitespace (%s)", name, routeAliasRulesNote)
			}
			s, ok := target.(string)
			if !ok || s == "" {
				return fmt.Errorf("platform config routeAliases[%q] must be a non-empty string, got %s (%s)", name, jsonKind(target), routeAliasRulesNote)
			}
			if strings.IndexFunc(s, unicode.IsSpace) >= 0 {
				return fmt.Errorf("platform config routeAliases[%q] = %q must not contain whitespace (%s)", name, s, routeAliasRulesNote)
			}
		}
	case "tenantAuth":
		if _, ok := value.(map[string]any); !ok {
			return fmt.Errorf("platform config tenantAuth must be a JSON object, got %s", jsonKind(value))
		}
	case "insecureRegistries":
		entries, ok := value.([]any)
		if !ok {
			return fmt.Errorf("platform config insecureRegistries must be a JSON array of registry host[:port] strings, got %s (%s)", jsonKind(value), insecureRegistryRulesNote)
		}
		for i, entry := range entries {
			s, ok := entry.(string)
			if !ok || s == "" {
				return fmt.Errorf("platform config insecureRegistries[%d] must be a non-empty string, got %s (%s)", i, jsonKind(entry), insecureRegistryRulesNote)
			}
			if strings.IndexFunc(s, unicode.IsSpace) >= 0 {
				return fmt.Errorf("platform config insecureRegistries[%d] = %q must not contain whitespace (%s)", i, s, insecureRegistryRulesNote)
			}
			if strings.Contains(s, "://") || strings.Contains(s, "/") {
				return fmt.Errorf("platform config insecureRegistries[%d] = %q must be a registry host[:port] as workload references spell it, without a scheme or path (%s)", i, s, insecureRegistryRulesNote)
			}
		}
	case "backingServiceClasses":
		classes, ok := value.([]any)
		if !ok {
			return fmt.Errorf("platform config backingServiceClasses must be a JSON array of class objects, got %s", jsonKind(value))
		}
		for i, class := range classes {
			if _, ok := class.(map[string]any); !ok {
				return fmt.Errorf("platform config backingServiceClasses[%d] must be a JSON object, got %s", i, jsonKind(class))
			}
		}
	case "seedDefaultBackingClasses":
		if _, ok := value.(bool); !ok {
			return fmt.Errorf("platform config seedDefaultBackingClasses must be a boolean, got %s", jsonKind(value))
		}
	}
	return nil
}

// insecureRegistriesWithoutSwitch reports a non-empty insecureRegistries when
// --allow-insecure-registries is off. The platform refuses that combination
// ("insecureRegistries requires insecureRegistry: true"), only after the claim and npm install.
func insecureRegistriesWithoutSwitch(value any, allow bool) error {
	if entries, ok := value.([]any); ok && len(entries) > 0 && !allow {
		return errors.New("platform config insecureRegistries needs --allow-insecure-registries on every up; without it the platform refuses the list")
	}
	return nil
}

// validateKeptValue checks a value of key kept from a previous up or set by hand with
// pulumi config set, and returns it decoded. Without --path, Pulumi stores an object or
// list as a JSON string, which config.getObject also parses, so that form is decoded
// for the check; the stack config keeps the value unchanged.
func validateKeptValue(key string, value any) (any, error) {
	// pulumi config set --secret stores {"secure": "v1:..."}; the platform reads these
	// keys as plain values, so a secret cannot be validated or used.
	if object, ok := value.(map[string]any); ok && len(object) == 1 {
		if _, secret := object["secure"]; secret {
			return nil, fmt.Errorf("saved stack config (Pulumi.dev.yaml): %s must not be a secret; set it without --secret or pass --platform-config", key)
		}
	}
	if text, ok := value.(string); ok {
		var decoded any
		if err := json.Unmarshal([]byte(text), &decoded); err != nil {
			return nil, fmt.Errorf("saved stack config %s is a string that is not valid JSON: %w", key, err)
		}
		value = decoded
	}
	if err := validatePlatformConfigValue(key, value); err != nil {
		return nil, fmt.Errorf("saved stack config (Pulumi.dev.yaml): %w; fix it there or pass --platform-config", err)
	}
	return value, nil
}

// readPlatformConfig reads and validates a --platform-config file. It writes nothing.
func readPlatformConfig(path string) (map[string]any, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	config := map[string]any{}
	if err := json.Unmarshal(b, &config); err != nil {
		return nil, fmt.Errorf("decode platform config JSON: %w", err)
	}
	if config == nil {
		return nil, errors.New("platform config must be a JSON object")
	}
	for k, v := range config {
		if !slices.Contains(platformConfigKeys, k) {
			return nil, fmt.Errorf("unsupported platform config key %q", k)
		}
		if err := validatePlatformConfigValue(k, v); err != nil {
			return nil, err
		}
	}
	return config, nil
}

func jsonKind(value any) string {
	switch v := value.(type) {
	case nil:
		return "null"
	case map[string]any:
		return "an object"
	case []any:
		return "an array"
	case string:
		if v == "" {
			return "an empty string"
		}
		return "a string"
	case float64:
		return "a number"
	case bool:
		return "a boolean"
	default:
		return fmt.Sprintf("%T", value)
	}
}

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
	// Validate the platform config before writing anything to the project.
	var fileConfig map[string]any
	if p.ConfigFile != "" {
		var err error
		if fileConfig, err = readPlatformConfig(p.ConfigFile); err != nil {
			return projectIdentity{}, err
		}
		if err := insecureRegistriesWithoutSwitch(fileConfig["insecureRegistries"], options.AllowInsecureRegistries); err != nil {
			return projectIdentity{}, err
		}
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
	if strings.HasPrefix(pkg, "file:") {
		local, err := vendorTarball(p.Directory, strings.TrimPrefix(pkg, "file:"))
		if err != nil {
			return identity, err
		}
		spec = local
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
		for _, key := range platformConfigKeys {
			if value, ok := stack.Config["di-framework-kube:"+key]; ok {
				config[key] = value
			}
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return identity, err
	}
	insecureRegistries := config["insecureRegistries"]
	if fileConfig != nil {
		config = fileConfig
		insecureRegistries = config["insecureRegistries"]
	} else {
		for _, key := range keptValidatedKeys {
			value, ok := config[key]
			if !ok {
				continue
			}
			decoded, err := validateKeptValue(key, value)
			if err != nil {
				return identity, err
			}
			if key == "insecureRegistries" {
				insecureRegistries = decoded
			}
		}
	}
	if err := insecureRegistriesWithoutSwitch(insecureRegistries, options.AllowInsecureRegistries); err != nil {
		return identity, err
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
	applyTenantHostImage(config)
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

// vendorTarball copies a platform tarball into the project under its content hash and
// returns the dependency spec for it. npm keeps an installed file: dependency while its
// spec is unchanged, so a tarball rebuilt at the same path and version would otherwise
// leave the previous build installed.
func vendorTarball(dir, tarball string) (string, error) {
	data, err := os.ReadFile(tarball)
	if err != nil {
		return "", fmt.Errorf("read platform tarball: %w", err)
	}
	sum := sha256.Sum256(data)
	name := "platform-" + hex.EncodeToString(sum[:])[:16] + ".tgz"
	vendor := filepath.Join(dir, "vendor")
	if err := os.MkdirAll(vendor, 0700); err != nil {
		return "", err
	}
	old, err := filepath.Glob(filepath.Join(vendor, "platform-*.tgz"))
	if err != nil {
		return "", err
	}
	for _, path := range old {
		if filepath.Base(path) != name {
			if err := os.Remove(path); err != nil {
				return "", err
			}
		}
	}
	if err := os.WriteFile(filepath.Join(vendor, name), data, 0600); err != nil {
		return "", err
	}
	return "file:vendor/" + name, nil
}
