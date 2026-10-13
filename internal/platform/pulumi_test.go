package platform

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"
)

func TestSharedProgramLifecycle(t *testing.T) {
	dir := t.TempDir()
	var commands []string
	var claims []bool
	p := Pulumi{Directory: dir, Kubeconfig: "/tmp/kube", Namespace: "wasmcloud", Server: "https://example.test"}
	p.run = func(_ context.Context, name string, args []string, cwd string, env []string) ([]byte, error) {
		if cwd != dir {
			t.Fatalf("cwd %s", cwd)
		}
		if !strings.Contains(strings.Join(env, "\n"), "PULUMI_BACKEND_URL=file:") {
			t.Fatal("missing backend")
		}
		commands = append(commands, name+" "+strings.Join(args, " "))
		return nil, nil
	}
	p.ownership = func(_ context.Context, i projectIdentity, release bool) error {
		if i.Owner == "" {
			t.Fatal("missing owner identity")
		}
		key, _ := os.ReadFile(filepath.Join(dir, ".passphrase"))
		if string(key) == i.Owner {
			t.Fatal("passphrase exposed as owner token")
		}
		claims = append(claims, release)
		return nil
	}
	configPath := filepath.Join(dir, "declarations.json")
	os.WriteFile(configPath, []byte(`{"tenants":[{"name":"alpha"}],"users":[]}`), 0600)
	p.ConfigFile = configPath
	opts := InstallOptions{Release: "wasmcloud", Chart: DefaultChart, ChartVersion: DefaultChartVersion, Timeout: time.Minute, NodePort: 30080}
	if err := p.Up(context.Background(), opts); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(commands[0], "npm install") || !strings.Contains(commands[1], "stack select dev --create") || !strings.Contains(commands[2], "up --stack dev") {
		t.Fatal(commands)
	}
	program, _ := os.ReadFile(filepath.Join(dir, "index.ts"))
	if string(program) != platformProgram || strings.Contains(string(program), "helm") {
		t.Fatal(string(program))
	}
	b, _ := os.ReadFile(filepath.Join(dir, "Pulumi.dev.yaml"))
	var stack map[string]any
	json.Unmarshal(b, &stack)
	config := stack["config"].(map[string]any)
	if config["di-framework-kube:httpNodePort"] != float64(30080) {
		t.Fatal(config)
	}
	stack["encryptionsalt"] = "preserve-me"
	b, _ = json.Marshal(stack)
	os.WriteFile(filepath.Join(dir, "Pulumi.dev.yaml"), b, 0600)
	p.ConfigFile = ""
	if _, err := p.prepare(opts); err != nil {
		t.Fatal(err)
	}
	b, _ = os.ReadFile(filepath.Join(dir, "Pulumi.dev.yaml"))
	if !strings.Contains(string(b), "alpha") {
		t.Fatal("lost tenant declarations on update")
	}
	if !strings.Contains(string(b), "preserve-me") {
		t.Fatal("lost secrets provider metadata")
	}
	if err := p.Destroy(context.Background(), time.Minute); err != nil {
		t.Fatal(err)
	}
	if len(claims) != 3 || !claims[2] {
		t.Fatal(claims)
	}
	if !strings.Contains(commands[3], "destroy --stack dev") {
		t.Fatal(commands)
	}
	for _, name := range []string{".passphrase", "identity.json", "Pulumi.dev.yaml"} {
		info, _ := os.Stat(filepath.Join(dir, name))
		if info.Mode().Perm() != 0600 {
			t.Fatal(name, info.Mode())
		}
	}
}

func TestDefaultTenantHostImage(t *testing.T) {
	dir := t.TempDir()
	p := Pulumi{Directory: dir, Kubeconfig: "/tmp/kube", Namespace: "wasmcloud"}
	opts := InstallOptions{Release: "wasmcloud"}
	if _, err := p.prepare(opts); err != nil {
		t.Fatal(err)
	}
	read := func() string {
		b, err := os.ReadFile(filepath.Join(dir, "Pulumi.dev.yaml"))
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	if !strings.Contains(read(), DefaultTenantHostImage) {
		t.Fatal(read())
	}
	configPath := filepath.Join(dir, "declarations.json")
	os.WriteFile(configPath, []byte(`{"tenantHostImage":"example/wash:local"}`), 0600)
	p.ConfigFile = configPath
	if _, err := p.prepare(opts); err != nil {
		t.Fatal(err)
	}
	got := read()
	if !strings.Contains(got, "example/wash:local") || strings.Contains(got, "ee89fd4b") {
		t.Fatal(got)
	}
}

func TestReplacesRetiredTenantHostImageDefault(t *testing.T) {
	previous := previousTenantHostImages
	previousTenantHostImages = []string{"ghcr.io/example/wash:retired"}
	t.Cleanup(func() { previousTenantHostImages = previous })
	dir := t.TempDir()
	saved := []byte("{\"config\":{\"di-framework-kube:tenantHostImage\":\"ghcr.io/example/wash:retired\"}}\n")
	if err := os.WriteFile(filepath.Join(dir, "Pulumi.dev.yaml"), saved, 0600); err != nil {
		t.Fatal(err)
	}
	p := Pulumi{Directory: dir, Kubeconfig: "/tmp/kube", Namespace: "wasmcloud"}
	if _, err := p.prepare(InstallOptions{Release: "wasmcloud"}); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "Pulumi.dev.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	got := string(b)
	if !strings.Contains(got, DefaultTenantHostImage) || strings.Contains(got, "wash:retired") {
		t.Fatal(got)
	}
}

func TestPlatformConfigPassesEgressDestinations(t *testing.T) {
	dir := t.TempDir()
	p := Pulumi{Directory: dir, Kubeconfig: "/tmp/kube", Namespace: "wasmcloud"}
	configPath := filepath.Join(dir, "declarations.json")
	os.WriteFile(configPath, []byte(`{"tenants":[],"egressAllowedDestinations":["10.43.250.25:1025"]}`), 0600)
	p.ConfigFile = configPath
	opts := InstallOptions{Release: "wasmcloud", Chart: DefaultChart, ChartVersion: DefaultChartVersion, Timeout: time.Minute}
	if _, err := p.prepare(opts); err != nil {
		t.Fatal(err)
	}
	read := func() []any {
		b, _ := os.ReadFile(filepath.Join(dir, "Pulumi.dev.yaml"))
		var stack map[string]any
		json.Unmarshal(b, &stack)
		destinations, _ := stack["config"].(map[string]any)["di-framework-kube:egressAllowedDestinations"].([]any)
		return destinations
	}
	if got := read(); len(got) != 1 || got[0] != "10.43.250.25:1025" {
		t.Fatal(got)
	}
	p.ConfigFile = ""
	if _, err := p.prepare(opts); err != nil {
		t.Fatal(err)
	}
	if got := read(); len(got) != 1 {
		t.Fatal("lost egress destinations on update", got)
	}
	os.WriteFile(configPath, []byte(`{"egressAllowed":[]}`), 0600)
	p.ConfigFile = configPath
	if _, err := p.prepare(opts); err == nil || !strings.Contains(err.Error(), `unsupported platform config key "egressAllowed"`) {
		t.Fatal(err)
	}
}

// objectKeyTestPulumi returns a Pulumi whose commands and ownership are stubbed,
// so Up only writes the project files.
func objectKeyTestPulumi(t *testing.T) (Pulumi, InstallOptions) {
	t.Helper()
	dir := t.TempDir()
	p := Pulumi{Directory: dir, Kubeconfig: "/tmp/kube", Namespace: "wasmcloud", Server: "https://example.test"}
	p.run = func(context.Context, string, []string, string, []string) ([]byte, error) { return nil, nil }
	p.ownership = func(context.Context, projectIdentity, bool) error { return nil }
	return p, InstallOptions{Release: "wasmcloud", Chart: DefaultChart, ChartVersion: DefaultChartVersion, Timeout: time.Minute}
}

func writePlatformConfig(t *testing.T, p *Pulumi, body string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "platform.json")
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	p.ConfigFile = path
}

func stackConfigValue(t *testing.T, dir, key string) any {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, "Pulumi.dev.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var stack struct {
		Config map[string]any `json:"config"`
	}
	if err := json.Unmarshal(b, &stack); err != nil {
		t.Fatal(err)
	}
	return stack.Config["di-framework-kube:"+key]
}

func decodeJSON(t *testing.T, s string) any {
	t.Helper()
	var v any
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		t.Fatal(err)
	}
	return v
}

const routeAliasesJSON = `{"identity.identity":"identity.identity-pg","console.console":"console.console-v2"}`

const tenantAuthJSON = `{"image":"ghcr.io/di-framework/tenant-auth@sha256:7a57d4241e876ef253bd50bd921a2a890af9dabc49842e8de465038a57b08f08","issuer":"http://stub-oidc.pg-stub-oidc.svc.cluster.local","oauthClient":{"id":"pgprove-console","secretName":"tenant-auth-oauth"},"issuerUpstreamPodPort":8080,"registry":{"component":"ghcr.io/di-framework/oci-registry:1"}}`

func TestPlatformConfigPassesRouteAliases(t *testing.T) {
	p, opts := objectKeyTestPulumi(t)
	writePlatformConfig(t, &p, `{"tenants":[],"routeAliases":`+routeAliasesJSON+`}`)
	if err := p.Up(context.Background(), opts); err != nil {
		t.Fatal(err)
	}
	// Written as a JSON object, the form config.getObject('routeAliases') reads.
	got := stackConfigValue(t, p.Directory, "routeAliases")
	if _, ok := got.(map[string]any); !ok {
		t.Fatalf("routeAliases written as %T, want a JSON object: %v", got, got)
	}
	if want := decodeJSON(t, routeAliasesJSON); !reflect.DeepEqual(got, want) {
		t.Fatalf("routeAliases = %v, want %v", got, want)
	}
}

func TestPlatformConfigPassesTenantAuth(t *testing.T) {
	p, opts := objectKeyTestPulumi(t)
	writePlatformConfig(t, &p, `{"tenants":[],"tenantAuth":`+tenantAuthJSON+`}`)
	if err := p.Up(context.Background(), opts); err != nil {
		t.Fatal(err)
	}
	got := stackConfigValue(t, p.Directory, "tenantAuth")
	if _, ok := got.(map[string]any); !ok {
		t.Fatalf("tenantAuth written as %T, want a JSON object: %v", got, got)
	}
	if want := decodeJSON(t, tenantAuthJSON); !reflect.DeepEqual(got, want) {
		t.Fatalf("tenantAuth = %v, want %v", got, want)
	}
}

func TestSecondUpKeepsRouteAliasesAndTenantAuth(t *testing.T) {
	p, opts := objectKeyTestPulumi(t)
	platformJSON := `{"tenants":[{"name":"alpha"}],"routeAliases":` + routeAliasesJSON + `,"tenantAuth":` + tenantAuthJSON + `}`
	writePlatformConfig(t, &p, platformJSON)
	if err := p.Up(context.Background(), opts); err != nil {
		t.Fatal(err)
	}
	check := func(stage string) {
		t.Helper()
		if got, want := stackConfigValue(t, p.Directory, "routeAliases"), decodeJSON(t, routeAliasesJSON); !reflect.DeepEqual(got, want) {
			t.Fatalf("%s: routeAliases = %v, want %v", stage, got, want)
		}
		if got, want := stackConfigValue(t, p.Directory, "tenantAuth"), decodeJSON(t, tenantAuthJSON); !reflect.DeepEqual(got, want) {
			t.Fatalf("%s: tenantAuth = %v, want %v", stage, got, want)
		}
	}
	check("first up")
	// Second up with the same platform.json.
	if err := p.Up(context.Background(), opts); err != nil {
		t.Fatal(err)
	}
	check("second up with --platform-config")
	// Another up without --platform-config preserves the saved declarations.
	p.ConfigFile = ""
	if err := p.Up(context.Background(), opts); err != nil {
		t.Fatal(err)
	}
	check("up without --platform-config")
	// A platform.json without routeAliases removes the alias (cutover rollback).
	writePlatformConfig(t, &p, `{"tenants":[{"name":"alpha"}],"tenantAuth":`+tenantAuthJSON+`}`)
	if err := p.Up(context.Background(), opts); err != nil {
		t.Fatal(err)
	}
	if got := stackConfigValue(t, p.Directory, "routeAliases"); got != nil {
		t.Fatalf("routeAliases kept after removal from platform.json: %v", got)
	}
}

func TestRejectsMalformedRouteAliases(t *testing.T) {
	for _, tc := range []struct {
		name, value, want string
	}{
		{"string", `"identity.identity=identity.identity-pg"`, "platform config routeAliases must be a JSON object mapping route names to route names, got a string"},
		{"array", `["identity.identity"]`, "platform config routeAliases must be a JSON object mapping route names to route names, got an array"},
		{"null", `null`, "platform config routeAliases must be a JSON object mapping route names to route names, got null"},
		{"number value", `{"identity.identity":1}`, `platform config routeAliases["identity.identity"] must be a non-empty string, got a number`},
		{"object value", `{"identity.identity":{"to":"identity.identity-pg"}}`, `platform config routeAliases["identity.identity"] must be a non-empty string, got an object`},
		{"empty value", `{"identity.identity":""}`, `platform config routeAliases["identity.identity"] must be a non-empty string, got an empty string`},
		{"empty name", `{"":"identity.identity-pg"}`, "platform config routeAliases has an empty route name"},
		{"uppercase name", `{"Identity.identity":"identity.identity-pg"}`, `platform config routeAliases route name "Identity.identity" must be <label>.<label> with lowercase DNS-1123 labels (reserved names and alias chains are checked by the platform during pulumi up)`},
		{"single-label name", `{"identity":"identity.identity-pg"}`, `platform config routeAliases route name "identity" must be <label>.<label> with lowercase DNS-1123 labels (reserved names and alias chains are checked by the platform during pulumi up)`},
		{"three-label name", `{"a.b.c":"identity.identity-pg"}`, `platform config routeAliases route name "a.b.c" must be <label>.<label> with lowercase DNS-1123 labels (reserved names and alias chains are checked by the platform during pulumi up)`},
		{"bad value label", `{"identity.identity":"identity.identity_pg"}`, `platform config routeAliases["identity.identity"] = "identity.identity_pg" must be <label>.<label> with lowercase DNS-1123 labels (reserved names and alias chains are checked by the platform during pulumi up)`},
		{"value label ends with hyphen", `{"identity.identity":"identity-.identity-pg"}`, `platform config routeAliases["identity.identity"] = "identity-.identity-pg" must be <label>.<label> with lowercase DNS-1123 labels (reserved names and alias chains are checked by the platform during pulumi up)`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, opts := objectKeyTestPulumi(t)
			p.run = func(context.Context, string, []string, string, []string) ([]byte, error) {
				t.Fatal("ran a command with malformed routeAliases")
				return nil, nil
			}
			writePlatformConfig(t, &p, `{"routeAliases":`+tc.value+`}`)
			err := p.Up(context.Background(), opts)
			if err == nil || err.Error() != tc.want {
				t.Fatalf("error = %v, want %q", err, tc.want)
			}
			assertProjectEmpty(t, p.Directory)
		})
	}
}

func TestRejectsNonObjectTenantAuth(t *testing.T) {
	p, opts := objectKeyTestPulumi(t)
	writePlatformConfig(t, &p, `{"tenantAuth":"ghcr.io/di-framework/tenant-auth:1"}`)
	if _, err := p.prepare(opts); err == nil || err.Error() != "platform config tenantAuth must be a JSON object, got a string" {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(p.Directory, "Pulumi.dev.yaml")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("wrote the stack config despite a non-object tenantAuth")
	}
	assertProjectEmpty(t, p.Directory)
}

// assertProjectEmpty checks that a rejected platform config left nothing behind:
// no state/, .passphrase, identity, stack config or vendored tarball.
func assertProjectEmpty(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		names := []string{}
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Fatalf("project directory not empty after rejection: %v", names)
	}
}

func TestRejectedConfigLeavesFreshProjectAbsent(t *testing.T) {
	p, opts := objectKeyTestPulumi(t)
	p.Directory = filepath.Join(p.Directory, "platform")
	tarball := filepath.Join(t.TempDir(), "di-framework-platform-6.0.2.tgz")
	if err := os.WriteFile(tarball, []byte("build"), 0600); err != nil {
		t.Fatal(err)
	}
	p.Package = "file:" + tarball
	writePlatformConfig(t, &p, `{"routeAliases":"x"}`)
	if _, err := p.prepare(opts); err == nil || !strings.Contains(err.Error(), "routeAliases must be a JSON object") {
		t.Fatal(err)
	}
	if _, err := os.Stat(p.Directory); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("created the project directory before validating the platform config: %v", err)
	}
}

// Phase D of proving-grounds sets tenantAuth by hand with
// `pulumi config set --path tenantAuth --json ...`, which leaves YAML.
const handSetStackYAML = `encryptionsalt: v1:abc
config:
  di-framework-kube:tenants:
    - name: alpha
  di-framework-kube:tenantAuth:
    image: ghcr.io/di-framework/tenant-auth@sha256:7a57d4241e876ef253bd50bd921a2a890af9dabc49842e8de465038a57b08f08
    issuer: http://stub-oidc.pg-stub-oidc.svc.cluster.local
    oauthClient:
      id: pgprove-console
      secretName: tenant-auth-oauth
    issuerUpstreamPodPort: 8080
    registry:
      component: ghcr.io/di-framework/oci-registry:1
  di-framework-kube:routeAliases:
    identity.identity: identity.identity-pg
    console.console: console.console-v2
`

func TestUpKeepsHandSetYAMLTenantAuth(t *testing.T) {
	p, opts := objectKeyTestPulumi(t)
	if err := os.WriteFile(filepath.Join(p.Directory, "Pulumi.dev.yaml"), []byte(handSetStackYAML), 0600); err != nil {
		t.Fatal(err)
	}
	if err := p.Up(context.Background(), opts); err != nil {
		t.Fatal(err)
	}
	if got, want := stackConfigValue(t, p.Directory, "tenantAuth"), decodeJSON(t, tenantAuthJSON); !reflect.DeepEqual(got, want) {
		t.Fatalf("tenantAuth = %v, want %v", got, want)
	}
	if got, want := stackConfigValue(t, p.Directory, "routeAliases"), decodeJSON(t, routeAliasesJSON); !reflect.DeepEqual(got, want) {
		t.Fatalf("routeAliases = %v, want %v", got, want)
	}
	b, _ := os.ReadFile(filepath.Join(p.Directory, "Pulumi.dev.yaml"))
	if !strings.Contains(string(b), "v1:abc") {
		t.Fatal("lost encryptionsalt")
	}
}

func TestUpValidatesKeptRouteAliases(t *testing.T) {
	for _, tc := range []struct {
		name, saved, want string
	}{
		{"JSON string form is accepted", `"{\"identity.identity\":\"identity.identity-pg\"}"`, ""},
		{"bad label", `{"identity.identity":"Identity.PG"}`, `saved stack config (Pulumi.dev.yaml): platform config routeAliases["identity.identity"] = "Identity.PG" must be <label>.<label> with lowercase DNS-1123 labels (reserved names and alias chains are checked by the platform during pulumi up); fix it there or pass --platform-config`},
		{"non-JSON string", `"identity.identity=identity.identity-pg"`, "saved stack config routeAliases is a string that is not a JSON object"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, opts := objectKeyTestPulumi(t)
			saved := `{"config":{"di-framework-kube:routeAliases":` + tc.saved + `}}`
			if err := os.WriteFile(filepath.Join(p.Directory, "Pulumi.dev.yaml"), []byte(saved), 0600); err != nil {
				t.Fatal(err)
			}
			_, err := p.prepare(opts)
			if tc.want == "" {
				if err != nil {
					t.Fatal(err)
				}
				// Passed through unchanged, in the form config.getObject parses.
				if got := stackConfigValue(t, p.Directory, "routeAliases"); got != `{"identity.identity":"identity.identity-pg"}` {
					t.Fatalf("routeAliases = %#v", got)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestRefusesChangedTargetAndUnownedCluster(t *testing.T) {
	p := Pulumi{Directory: t.TempDir(), Kubeconfig: "/tmp/kube", Namespace: "wasmcloud"}
	opts := InstallOptions{Release: "wasmcloud"}
	if _, err := p.prepare(opts); err != nil {
		t.Fatal(err)
	}
	p.Namespace = "other"
	if _, err := p.prepare(opts); err == nil {
		t.Fatal("accepted target change")
	}
	p.Namespace = "wasmcloud"
	p.run = func(context.Context, string, []string, string, []string) ([]byte, error) {
		t.Fatal("ran command before ownership")
		return nil, nil
	}
	p.ownership = func(context.Context, projectIdentity, bool) error { return errors.New("another owner") }
	if err := p.Up(context.Background(), opts); err == nil || !strings.Contains(err.Error(), "another owner") {
		t.Fatal(err)
	}
}

func TestFailedDestroyKeepsClaim(t *testing.T) {
	p := Pulumi{Directory: t.TempDir()}
	p.prepare(InstallOptions{})
	p.run = func(context.Context, string, []string, string, []string) ([]byte, error) {
		return nil, errors.New("destroy failed")
	}
	p.ownership = func(_ context.Context, _ projectIdentity, release bool) error {
		if release {
			t.Fatal("released failed destroy")
		}
		return nil
	}
	if err := p.Destroy(context.Background(), time.Minute); err == nil {
		t.Fatal("expected error")
	}
}

func TestDestroyChecksToolsBeforeOwnershipChanges(t *testing.T) {
	for _, missing := range []string{"pulumi", "node"} {
		t.Run(missing, func(t *testing.T) {
			bin := t.TempDir()
			for _, name := range []string{"pulumi", "node"} {
				if name != missing {
					if err := os.WriteFile(filepath.Join(bin, name), []byte("#!/bin/sh\nexit 97\n"), 0700); err != nil {
						t.Fatal(err)
					}
				}
			}
			t.Setenv("PATH", bin)
			p := Pulumi{Directory: t.TempDir(), ownership: func(context.Context, projectIdentity, bool) error {
				t.Fatal("changed ownership before checking prerequisites")
				return nil
			}}
			err := p.Destroy(context.Background(), time.Minute)
			if err == nil || !strings.Contains(err.Error(), "shared platform requires "+missing+" on PATH") {
				t.Fatalf("unexpected error: %v", err)
			}
			if !errors.Is(err, exec.ErrNotFound) {
				t.Fatalf("missing underlying lookup error: %v", err)
			}
		})
	}
}

func TestDestroyDoesNotRequireNpm(t *testing.T) {
	bin := t.TempDir()
	for _, name := range []string{"pulumi", "node"} {
		if err := os.WriteFile(filepath.Join(bin, name), []byte("#!/bin/sh\nexit 97\n"), 0700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "identity.json"), []byte(`{"owner":"test-owner"}`), 0600); err != nil {
		t.Fatal(err)
	}
	reachedOwnership := errors.New("reached ownership validation")
	p := Pulumi{Directory: dir, ownership: func(context.Context, projectIdentity, bool) error { return reachedOwnership }}
	if err := p.Destroy(context.Background(), time.Minute); !errors.Is(err, reachedOwnership) {
		t.Fatalf("cleanup was blocked before ownership validation: %v", err)
	}
}

func TestRebuiltTarballChangesDependency(t *testing.T) {
	dir := t.TempDir()
	tarball := filepath.Join(t.TempDir(), "di-framework-platform-6.0.2.tgz")
	p := Pulumi{Directory: dir, Kubeconfig: "/tmp/kube", Namespace: "wasmcloud", Server: "https://example.test", Package: "file:" + tarball}
	opts := InstallOptions{Release: "wasmcloud", Chart: DefaultChart, ChartVersion: DefaultChartVersion, Timeout: time.Minute}
	dependency := func() string {
		b, err := os.ReadFile(filepath.Join(dir, "package.json"))
		if err != nil {
			t.Fatal(err)
		}
		var manifest struct {
			Dependencies map[string]string `json:"dependencies"`
		}
		if err := json.Unmarshal(b, &manifest); err != nil {
			t.Fatal(err)
		}
		return manifest.Dependencies["@di-framework/platform"]
	}
	os.WriteFile(tarball, []byte("first build"), 0600)
	if _, err := p.prepare(opts); err != nil {
		t.Fatal(err)
	}
	first := dependency()
	if !strings.HasPrefix(first, "file:vendor/platform-") {
		t.Fatal(first)
	}
	if _, err := p.prepare(opts); err != nil {
		t.Fatal(err)
	}
	if dependency() != first {
		t.Fatal("unchanged tarball changed the dependency")
	}
	// Same path and version, new content: npm must see a different dependency.
	os.WriteFile(tarball, []byte("second build"), 0600)
	if _, err := p.prepare(opts); err != nil {
		t.Fatal(err)
	}
	second := dependency()
	if second == first {
		t.Fatal("rebuilt tarball kept the cached dependency")
	}
	vendored, _ := filepath.Glob(filepath.Join(dir, "vendor", "platform-*.tgz"))
	if len(vendored) != 1 || "file:vendor/"+filepath.Base(vendored[0]) != second {
		t.Fatal(vendored)
	}
	if b, _ := os.ReadFile(vendored[0]); string(b) != "second build" {
		t.Fatal(string(b))
	}
	p.Package = "file:" + filepath.Join(t.TempDir(), "missing.tgz")
	if _, err := p.prepare(opts); err == nil || !strings.Contains(err.Error(), "read platform tarball") {
		t.Fatal(err)
	}
}

func TestDefaultPlatformPackageIsPinned(t *testing.T) {
	if !regexp.MustCompile(`^@di-framework/platform@6\.0\.[0-9]+$`).MatchString(DefaultPlatformPackage) {
		t.Fatal(DefaultPlatformPackage)
	}
}
