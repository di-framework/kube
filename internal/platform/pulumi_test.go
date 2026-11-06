package platform

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strconv"
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
		{"string", `"identity.identity=identity.identity-pg"`, "platform config routeAliases must be a JSON object mapping route names to route names, got a string (route grammar, reserved names and alias chains are checked by the platform during pulumi up)"},
		{"array", `["identity.identity"]`, "platform config routeAliases must be a JSON object mapping route names to route names, got an array (route grammar, reserved names and alias chains are checked by the platform during pulumi up)"},
		{"null", `null`, "platform config routeAliases must be a JSON object mapping route names to route names, got null (route grammar, reserved names and alias chains are checked by the platform during pulumi up)"},
		{"number value", `{"identity.identity":1}`, `platform config routeAliases["identity.identity"] must be a non-empty string, got a number (route grammar, reserved names and alias chains are checked by the platform during pulumi up)`},
		{"object value", `{"identity.identity":{"to":"identity.identity-pg"}}`, `platform config routeAliases["identity.identity"] must be a non-empty string, got an object (route grammar, reserved names and alias chains are checked by the platform during pulumi up)`},
		{"empty value", `{"identity.identity":""}`, `platform config routeAliases["identity.identity"] must be a non-empty string, got an empty string (route grammar, reserved names and alias chains are checked by the platform during pulumi up)`},
		{"empty name", `{"":"identity.identity-pg"}`, "platform config routeAliases has an empty route name (route grammar, reserved names and alias chains are checked by the platform during pulumi up)"},
		{"space in name", `{"identity .identity":"identity.identity-pg"}`, `platform config routeAliases route name "identity .identity" must not contain whitespace (route grammar, reserved names and alias chains are checked by the platform during pulumi up)`},
		{"newline in value", `{"identity.identity":"identity.identity-pg\n"}`, `platform config routeAliases["identity.identity"] = "identity.identity-pg\n" must not contain whitespace (route grammar, reserved names and alias chains are checked by the platform during pulumi up)`},
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

func TestAcceptsPlatformRouteGrammar(t *testing.T) {
	// The platform owns route grammar; up must not be stricter than it.
	aliases := `{"api.v2.alpha":"api.v3.alpha","my_app.alpha":"my_app2.alpha","Identity.Identity":"identity.identity-pg","a.b.c":"x.y.z"}`
	p, opts := objectKeyTestPulumi(t)
	writePlatformConfig(t, &p, `{"routeAliases":`+aliases+`}`)
	if err := p.Up(context.Background(), opts); err != nil {
		t.Fatal(err)
	}
	if got, want := stackConfigValue(t, p.Directory, "routeAliases"), decodeJSON(t, aliases); !reflect.DeepEqual(got, want) {
		t.Fatalf("routeAliases = %v, want %v", got, want)
	}
	// The same aliases kept in the stack config pass an up without --platform-config.
	p.ConfigFile = ""
	if err := p.Up(context.Background(), opts); err != nil {
		t.Fatal(err)
	}
	if got, want := stackConfigValue(t, p.Directory, "routeAliases"), decodeJSON(t, aliases); !reflect.DeepEqual(got, want) {
		t.Fatalf("kept routeAliases = %v, want %v", got, want)
	}
}

func TestUpValidatesKeptRouteAliases(t *testing.T) {
	for _, tc := range []struct {
		name, saved, want string
	}{
		{"JSON string form is accepted", `"{\"identity.identity\":\"identity.identity-pg\"}"`, ""},
		{"non-string value", `{"identity.identity":1}`, `saved stack config (Pulumi.dev.yaml): platform config routeAliases["identity.identity"] must be a non-empty string, got a number (route grammar, reserved names and alias chains are checked by the platform during pulumi up); fix it there or pass --platform-config`},
		{"secret", `{"secure":"v1:c2VjcmV0"}`, "saved stack config (Pulumi.dev.yaml): routeAliases must not be a secret; set it without --secret or pass --platform-config"},
		{"non-JSON string", `"identity.identity=identity.identity-pg"`, "saved stack config routeAliases is a string that is not valid JSON"},
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

// parseVersion parses an exact MAJOR.MINOR.PATCH release.
func parseVersion(t *testing.T, version string) platformVersion {
	t.Helper()
	m := regexp.MustCompile(`^([0-9]+)\.([0-9]+)\.([0-9]+)$`).FindStringSubmatch(version)
	if m == nil {
		t.Fatalf("not an exact MAJOR.MINOR.PATCH version: %q", version)
	}
	var n [3]int
	for i := range n {
		v, err := strconv.Atoi(m[i+1])
		if err != nil {
			t.Fatal(err)
		}
		n[i] = v
	}
	return releaseVersion(n[0], n[1], n[2])
}

func TestDefaultPlatformPackageIsPinned(t *testing.T) {
	// 6.0.29 is the release whose DEFAULT_HOST_IMAGE is DefaultTenantHostImage, the first
	// that lists it for NATS credentials; any exact later version passes.
	version, ok := strings.CutPrefix(DefaultPlatformPackage, "@di-framework/platform@")
	if !ok {
		t.Fatal(DefaultPlatformPackage)
	}
	if compareVersions(parseVersion(t, version), parseVersion(t, "6.0.29")) < 0 {
		t.Fatalf("DefaultPlatformPackage %s is older than 6.0.29", DefaultPlatformPackage)
	}
	current := cliDefaultHostImages[len(cliDefaultHostImages)-1]
	if current.image != DefaultTenantHostImage || !current.listed {
		t.Fatalf("cliDefaultHostImages ends in %+v, want the listed current default", current)
	}
	if compareVersions(parseVersion(t, version), current.since) < 0 {
		t.Fatalf("DefaultPlatformPackage %s predates %s, the first release that lists its host image", DefaultPlatformPackage, current.since)
	}
}

func TestCLIDefaultHostImagesTable(t *testing.T) {
	// The table is previousTenantHostImages then DefaultTenantHostImage, oldest first, with
	// the listing releases read from the platform's registry-nats.ts at each tag.
	var images []string
	for _, d := range cliDefaultHostImages {
		images = append(images, d.image)
	}
	want := append(slices.Clone(previousTenantHostImages), DefaultTenantHostImage)
	if !slices.Equal(images, want) {
		t.Fatalf("cliDefaultHostImages = %v, want %v", images, want)
	}
	listed := map[string]string{
		"sha256:8f0ee9c5a19ae0433e83a553431f1728ddf0a4787059c10e511209263d294537": "6.0.20",
		"sha256:bbe9587c508079074d43f4c27c53eadd92ed2825dc3541b7bcbc1f79a4e843a0": "6.0.29",
	}
	var last platformVersion
	for _, d := range cliDefaultHostImages {
		digest := d.image[strings.LastIndex(d.image, "@")+1:]
		since, ok := listed[digest]
		if ok != d.listed {
			t.Fatalf("%s listed = %v, want %v", d.image, d.listed, ok)
		}
		if !ok {
			continue
		}
		if d.since != parseVersion(t, since) {
			t.Fatalf("%s since = %s, want %s", d.image, d.since, since)
		}
		if compareVersions(d.since, last) <= 0 {
			t.Fatalf("listing releases are not increasing at %s", d.image)
		}
		last = d.since
	}
}

func TestListedDefaultHostImageFor(t *testing.T) {
	previous := "ghcr.io/di-framework/wash:2.8.0-wasi-tls@" + platform6028HostDigest
	for _, tc := range []struct {
		version string
		want    string
	}{
		{"6.0.13", ""},
		{"6.0.19", ""},
		{"6.0.20-rc.1", ""},
		{"6.0.20", previous},
		{"6.0.28", previous},
		{"6.0.29-rc.1", previous},
		{"6.0.29", DefaultTenantHostImage},
		{"6.1.0", DefaultTenantHostImage},
		{"7.0.0-rc.1", DefaultTenantHostImage},
	} {
		v, ok := platformPackageVersion("@di-framework/platform@" + tc.version)
		if !ok {
			t.Fatal(tc.version)
		}
		if got := listedDefaultHostImageFor(v); got != tc.want {
			t.Fatalf("listedDefaultHostImageFor(%s) = %q, want %q", tc.version, got, tc.want)
		}
	}
}

func TestPlatformPackageVersion(t *testing.T) {
	for _, tc := range []struct {
		pkg  string
		want platformVersion
		ok   bool
	}{
		{"@di-framework/platform@6.0.29", releaseVersion(6, 0, 29), true},
		{"@di-framework/platform@6.0.100", releaseVersion(6, 0, 100), true},
		// A pre-release orders below its release.
		{"@di-framework/platform@7.1.2-rc.1", platformVersion{7, 1, 2, 0}, true},
		{"@di-framework/platform@6.0", platformVersion{}, false},
		{"@di-framework/platform@6.0.29-", platformVersion{}, false},
		{"@di-framework/platform@^6.0.29", platformVersion{}, false},
		{"@di-framework/platform@latest", platformVersion{}, false},
		{"file:/tmp/di-framework-platform-6.0.29.tgz", platformVersion{}, false},
		{"6.0.29", platformVersion{}, false},
		{"", platformVersion{}, false},
	} {
		got, ok := platformPackageVersion(tc.pkg)
		if got != tc.want || ok != tc.ok {
			t.Fatalf("platformPackageVersion(%q) = %v, %v, want %v, %v", tc.pkg, got, ok, tc.want, tc.ok)
		}
	}
}

func TestPreReleaseOrdersBelowRelease(t *testing.T) {
	pre, ok := platformPackageVersion("@di-framework/platform@6.0.29-rc.1")
	if !ok {
		t.Fatal(pre)
	}
	if compareVersions(pre, releaseVersion(6, 0, 29)) >= 0 || compareVersions(pre, releaseVersion(6, 0, 28)) <= 0 {
		t.Fatalf("6.0.29-rc.1 = %v does not order between 6.0.28 and 6.0.29", pre)
	}
	if pre.String() != "6.0.29-pre" || releaseVersion(6, 0, 29).String() != "6.0.29" {
		t.Fatal(pre.String(), releaseVersion(6, 0, 29).String())
	}
}

func TestCompareVersions(t *testing.T) {
	for _, tc := range []struct {
		a, b string
		want int
	}{
		{"6.0.13", "6.0.13", 0},
		{"6.0.14", "6.0.13", 1},
		{"6.0.100", "6.0.13", 1},
		{"6.1.0", "6.0.13", 1},
		{"7.0.0", "6.0.13", 1},
		{"6.0.12", "6.0.13", -1},
		{"6.0.2", "6.0.13", -1},
		{"5.9.99", "6.0.13", -1},
	} {
		got := compareVersions(parseVersion(t, tc.a), parseVersion(t, tc.b))
		if (got > 0) != (tc.want > 0) || (got < 0) != (tc.want < 0) {
			t.Fatalf("compare(%s, %s) = %d, want sign %d", tc.a, tc.b, got, tc.want)
		}
	}
}

func TestAnnouncesPreviousDefaultImageSwap(t *testing.T) {
	// Every earlier CLI default is announced and replaced, not only the oldest.
	if len(previousTenantHostImages) < 3 {
		t.Fatalf("previousTenantHostImages = %v, want the ee89fd4b, d88158b0 and 8f0ee9c5 defaults", previousTenantHostImages)
	}
	for _, old := range previousTenantHostImages {
		notice := "notice: tenantHostImage " + old + " is an earlier di-framework-kube default; replacing it with " + DefaultTenantHostImage + ", which rolls every tenant host\n"
		t.Run("saved "+old[strings.LastIndex(old, ":")+1:][:8], func(t *testing.T) {
			p, opts := objectKeyTestPulumi(t)
			var stderr bytes.Buffer
			p.Stderr = &stderr
			if err := os.WriteFile(filepath.Join(p.Directory, "Pulumi.dev.yaml"), []byte(`{"config":{"di-framework-kube:tenantHostImage":"`+old+`"}}`), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := p.prepare(opts); err != nil {
				t.Fatal(err)
			}
			if stderr.String() != notice {
				t.Fatalf("stderr = %q, want %q", stderr.String(), notice)
			}
			if got := stackConfigValue(t, p.Directory, "tenantHostImage"); got != DefaultTenantHostImage {
				t.Fatal(got)
			}
			// The next up has nothing to swap and says nothing.
			stderr.Reset()
			if _, err := p.prepare(opts); err != nil {
				t.Fatal(err)
			}
			if stderr.Len() != 0 {
				t.Fatalf("second up printed %q", stderr.String())
			}
		})
		t.Run("platform.json "+old[strings.LastIndex(old, ":")+1:][:8], func(t *testing.T) {
			p, opts := objectKeyTestPulumi(t)
			var stderr bytes.Buffer
			p.Stderr = &stderr
			writePlatformConfig(t, &p, `{"tenantHostImage":"`+old+`"}`)
			if _, err := p.prepare(opts); err != nil {
				t.Fatal(err)
			}
			if stderr.String() != notice {
				t.Fatalf("stderr = %q, want %q", stderr.String(), notice)
			}
			if got := stackConfigValue(t, p.Directory, "tenantHostImage"); got != DefaultTenantHostImage {
				t.Fatal(got)
			}
		})
	}
	t.Run("other images are not announced", func(t *testing.T) {
		for _, image := range []string{"", DefaultTenantHostImage, "example/wash:local", "ghcr.io/di-framework/wash@" + oldHostDigest} {
			p, opts := objectKeyTestPulumi(t)
			var stderr bytes.Buffer
			p.Stderr = &stderr
			if image != "" {
				writePlatformConfig(t, &p, `{"tenantHostImage":"`+image+`"}`)
			}
			if _, err := p.prepare(opts); err != nil {
				t.Fatal(err)
			}
			if strings.Contains(stderr.String(), "notice:") {
				t.Fatalf("%q: stderr = %q", image, stderr.String())
			}
		}
	})
}

const insecureRegistriesJSON = `["examples-registry.wasmcloud.svc.cluster.local:5000","registry.local"]`

func insecureOpts(opts InstallOptions) InstallOptions {
	opts.AllowInsecureRegistries = true
	return opts
}

func TestPlatformConfigPassesInsecureRegistries(t *testing.T) {
	p, opts := objectKeyTestPulumi(t)
	opts = insecureOpts(opts)
	writePlatformConfig(t, &p, `{"tenants":[],"insecureRegistries":`+insecureRegistriesJSON+`}`)
	if err := p.Up(context.Background(), opts); err != nil {
		t.Fatal(err)
	}
	want := decodeJSON(t, insecureRegistriesJSON)
	check := func(stage string) {
		t.Helper()
		got := stackConfigValue(t, p.Directory, "insecureRegistries")
		if _, ok := got.([]any); !ok {
			t.Fatalf("%s: insecureRegistries written as %T, want a JSON array: %v", stage, got, got)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("%s: insecureRegistries = %v, want %v", stage, got, want)
		}
		if stackConfigValue(t, p.Directory, "insecureRegistry") != true {
			t.Fatalf("%s: insecureRegistry not set", stage)
		}
	}
	check("first up")
	// A second up without --platform-config keeps the list.
	p.ConfigFile = ""
	if err := p.Up(context.Background(), opts); err != nil {
		t.Fatal(err)
	}
	check("up without --platform-config")
	// A platform.json without the key removes it, like the other declarations.
	writePlatformConfig(t, &p, `{"tenants":[]}`)
	if err := p.Up(context.Background(), opts); err != nil {
		t.Fatal(err)
	}
	if got := stackConfigValue(t, p.Directory, "insecureRegistries"); got != nil {
		t.Fatalf("insecureRegistries kept after removal from platform.json: %v", got)
	}
}

// pgprove-style hand-set list: `pulumi config set --path 'insecureRegistries[0]' <host>` leaves YAML.
const handSetInsecureRegistriesYAML = `encryptionsalt: v1:abc
config:
  di-framework-kube:tenants: []
  di-framework-kube:insecureRegistries:
    - examples-registry.wasmcloud.svc.cluster.local:5000
    - registry.local
`

// examplesRegistry is the entry kube's examples pass with --insecure-registry (kube#8).
const examplesRegistry = "examples-registry.wasmcloud.svc.cluster.local:5000"

func registryOpts(opts InstallOptions, entries ...string) InstallOptions {
	opts = insecureOpts(opts)
	opts.InsecureRegistries = entries
	return opts
}

func TestUpAddsInsecureRegistryFlag(t *testing.T) {
	t.Run("fresh instance", func(t *testing.T) {
		p, opts := objectKeyTestPulumi(t)
		if err := p.Up(context.Background(), registryOpts(opts, examplesRegistry)); err != nil {
			t.Fatal(err)
		}
		if got, want := stackConfigValue(t, p.Directory, "insecureRegistries"), []any{examplesRegistry}; !reflect.DeepEqual(got, want) {
			t.Fatalf("insecureRegistries = %#v, want %#v", got, want)
		}
		if stackConfigValue(t, p.Directory, "insecureRegistry") != true {
			t.Fatal("insecureRegistry not set")
		}
		// Later ups keep it, with or without the flag, and never duplicate it.
		for _, next := range []InstallOptions{insecureOpts(opts), registryOpts(opts, examplesRegistry)} {
			if err := p.Up(context.Background(), next); err != nil {
				t.Fatal(err)
			}
			if got, want := stackConfigValue(t, p.Directory, "insecureRegistries"), []any{examplesRegistry}; !reflect.DeepEqual(got, want) {
				t.Fatalf("insecureRegistries = %#v after a later up, want %#v", got, want)
			}
		}
	})
	t.Run("added to the platform.json list", func(t *testing.T) {
		p, opts := objectKeyTestPulumi(t)
		writePlatformConfig(t, &p, `{"tenants":[],"insecureRegistries":["registry.local"]}`)
		if err := p.Up(context.Background(), registryOpts(opts, examplesRegistry, "registry.local")); err != nil {
			t.Fatal(err)
		}
		want := []any{"registry.local", examplesRegistry}
		if got := stackConfigValue(t, p.Directory, "insecureRegistries"); !reflect.DeepEqual(got, want) {
			t.Fatalf("insecureRegistries = %#v, want %#v", got, want)
		}
		if got := stackConfigValue(t, p.Directory, "tenants"); !reflect.DeepEqual(got, []any{}) {
			t.Fatalf("tenants = %#v, want the declarations from platform.json", got)
		}
		// A platform.json without the key and no flag removes the list, as before.
		writePlatformConfig(t, &p, `{"tenants":[]}`)
		if err := p.Up(context.Background(), insecureOpts(opts)); err != nil {
			t.Fatal(err)
		}
		if got := stackConfigValue(t, p.Directory, "insecureRegistries"); got != nil {
			t.Fatalf("insecureRegistries kept after removal from platform.json: %v", got)
		}
	})
	t.Run("added to a hand-set list", func(t *testing.T) {
		p, opts := objectKeyTestPulumi(t)
		if err := os.WriteFile(filepath.Join(p.Directory, "Pulumi.dev.yaml"), []byte(handSetInsecureRegistriesYAML), 0600); err != nil {
			t.Fatal(err)
		}
		if err := p.Up(context.Background(), registryOpts(opts, "mirror.local:5000", examplesRegistry)); err != nil {
			t.Fatal(err)
		}
		want := []any{examplesRegistry, "registry.local", "mirror.local:5000"}
		if got := stackConfigValue(t, p.Directory, "insecureRegistries"); !reflect.DeepEqual(got, want) {
			t.Fatalf("insecureRegistries = %#v, want %#v", got, want)
		}
	})
	t.Run("added to the JSON-string form", func(t *testing.T) {
		// Without --path, pulumi config set stores the list as a JSON string.
		p, opts := objectKeyTestPulumi(t)
		saved := `{"config":{"di-framework-kube:insecureRegistries":"[\"registry.local:5000\"]"}}`
		if err := os.WriteFile(filepath.Join(p.Directory, "Pulumi.dev.yaml"), []byte(saved), 0600); err != nil {
			t.Fatal(err)
		}
		if err := p.Up(context.Background(), registryOpts(opts, examplesRegistry)); err != nil {
			t.Fatal(err)
		}
		want := []any{"registry.local:5000", examplesRegistry}
		if got := stackConfigValue(t, p.Directory, "insecureRegistries"); !reflect.DeepEqual(got, want) {
			t.Fatalf("insecureRegistries = %#v, want %#v", got, want)
		}
	})
	t.Run("needs the switch", func(t *testing.T) {
		p, opts := objectKeyTestPulumi(t)
		opts.InsecureRegistries = []string{examplesRegistry}
		want := "--insecure-registry needs --allow-insecure-registries on every up; without it the platform refuses the list"
		if err := p.Up(context.Background(), opts); err == nil || err.Error() != want {
			t.Fatalf("error = %v, want %q", err, want)
		}
		if _, err := os.Stat(filepath.Join(p.Directory, "Pulumi.dev.yaml")); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("stack config written without the switch")
		}
	})
	t.Run("malformed entries", func(t *testing.T) {
		note := " (the exact host[:port] grammar is checked by the platform during pulumi up)"
		for _, tc := range []struct{ entry, want string }{
			{"", "--insecure-registry must be a registry host[:port], got an empty string" + note},
			{"registry.local :5000", `--insecure-registry "registry.local :5000" must not contain whitespace` + note},
			{"http://registry.local:5000", `--insecure-registry "http://registry.local:5000" must be a registry host[:port] as workload references spell it, without a scheme or path` + note},
			{"registry.local:5000/examples", `--insecure-registry "registry.local:5000/examples" must be a registry host[:port] as workload references spell it, without a scheme or path` + note},
		} {
			p, opts := objectKeyTestPulumi(t)
			if err := p.Up(context.Background(), registryOpts(opts, tc.entry)); err == nil || err.Error() != tc.want {
				t.Fatalf("%q: error = %v, want %q", tc.entry, err, tc.want)
			}
			if _, err := os.Stat(filepath.Join(p.Directory, "Pulumi.dev.yaml")); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("%q: stack config written", tc.entry)
			}
		}
	})
}

func TestUpKeepsHandSetYAMLInsecureRegistries(t *testing.T) {
	p, opts := objectKeyTestPulumi(t)
	if err := os.WriteFile(filepath.Join(p.Directory, "Pulumi.dev.yaml"), []byte(handSetInsecureRegistriesYAML), 0600); err != nil {
		t.Fatal(err)
	}
	if err := p.Up(context.Background(), insecureOpts(opts)); err != nil {
		t.Fatal(err)
	}
	if got, want := stackConfigValue(t, p.Directory, "insecureRegistries"), decodeJSON(t, insecureRegistriesJSON); !reflect.DeepEqual(got, want) {
		t.Fatalf("insecureRegistries = %v, want %v", got, want)
	}
}

func TestRejectsMalformedInsecureRegistries(t *testing.T) {
	note := " (the exact host[:port] grammar is checked by the platform during pulumi up)"
	for _, tc := range []struct {
		name, value, want string
	}{
		{"string", `"registry.local:5000"`, "platform config insecureRegistries must be a JSON array of registry host[:port] strings, got a string" + note},
		{"object", `{"host":"registry.local"}`, "platform config insecureRegistries must be a JSON array of registry host[:port] strings, got an object" + note},
		{"null", `null`, "platform config insecureRegistries must be a JSON array of registry host[:port] strings, got null" + note},
		{"number entry", `[5000]`, "platform config insecureRegistries[0] must be a non-empty string, got a number" + note},
		{"empty entry", `["registry.local",""]`, "platform config insecureRegistries[1] must be a non-empty string, got an empty string" + note},
		{"whitespace", `["registry.local :5000"]`, `platform config insecureRegistries[0] = "registry.local :5000" must not contain whitespace` + note},
		{"scheme", `["http://registry.local:5000"]`, `platform config insecureRegistries[0] = "http://registry.local:5000" must be a registry host[:port] as workload references spell it, without a scheme or path` + note},
		{"path", `["registry.local:5000/components"]`, `platform config insecureRegistries[0] = "registry.local:5000/components" must be a registry host[:port] as workload references spell it, without a scheme or path` + note},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, opts := objectKeyTestPulumi(t)
			p.run = func(context.Context, string, []string, string, []string) ([]byte, error) {
				t.Fatal("ran a command with malformed insecureRegistries")
				return nil, nil
			}
			writePlatformConfig(t, &p, `{"insecureRegistries":`+tc.value+`}`)
			err := p.Up(context.Background(), insecureOpts(opts))
			if err == nil || err.Error() != tc.want {
				t.Fatalf("error = %v, want %q", err, tc.want)
			}
			assertProjectEmpty(t, p.Directory)
		})
	}
}

func TestInsecureRegistriesNeedsSwitch(t *testing.T) {
	want := "platform config insecureRegistries needs --allow-insecure-registries on every up; without it the platform refuses the list"
	t.Run("from platform.json", func(t *testing.T) {
		p, opts := objectKeyTestPulumi(t)
		writePlatformConfig(t, &p, `{"insecureRegistries":`+insecureRegistriesJSON+`}`)
		if _, err := p.prepare(opts); err == nil || err.Error() != want {
			t.Fatalf("error = %v, want %q", err, want)
		}
		assertProjectEmpty(t, p.Directory)
	})
	t.Run("kept", func(t *testing.T) {
		p, opts := objectKeyTestPulumi(t)
		if err := os.WriteFile(filepath.Join(p.Directory, "Pulumi.dev.yaml"), []byte(handSetInsecureRegistriesYAML), 0600); err != nil {
			t.Fatal(err)
		}
		keptWant := "saved stack config (Pulumi.dev.yaml) insecureRegistries needs --allow-insecure-registries on every up; without it the platform refuses the list. Pass the flag, remove insecureRegistries from Pulumi.dev.yaml, or pass a --platform-config without it"
		if _, err := p.prepare(opts); err == nil || err.Error() != keptWant {
			t.Fatalf("error = %v, want %q", err, keptWant)
		}
		// A --platform-config without the list replaces the kept one.
		writePlatformConfig(t, &p, `{"tenants":[]}`)
		if _, err := p.prepare(opts); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("empty list needs no switch", func(t *testing.T) {
		p, opts := objectKeyTestPulumi(t)
		writePlatformConfig(t, &p, `{"insecureRegistries":[]}`)
		if _, err := p.prepare(opts); err != nil {
			t.Fatal(err)
		}
	})
}

func TestUpValidatesKeptInsecureRegistries(t *testing.T) {
	for _, tc := range []struct {
		name, saved, want string
	}{
		{"JSON string form is accepted", `"[\"registry.local:5000\"]"`, ""},
		{"scheme", `["http://registry.local:5000"]`, `saved stack config (Pulumi.dev.yaml): platform config insecureRegistries[0] = "http://registry.local:5000" must be a registry host[:port] as workload references spell it, without a scheme or path (the exact host[:port] grammar is checked by the platform during pulumi up); fix it there or pass --platform-config`},
		{"secret", `{"secure":"v1:c2VjcmV0"}`, "saved stack config (Pulumi.dev.yaml): insecureRegistries must not be a secret; set it without --secret or pass --platform-config"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, opts := objectKeyTestPulumi(t)
			saved := `{"config":{"di-framework-kube:insecureRegistries":` + tc.saved + `}}`
			if err := os.WriteFile(filepath.Join(p.Directory, "Pulumi.dev.yaml"), []byte(saved), 0600); err != nil {
				t.Fatal(err)
			}
			_, err := p.prepare(insecureOpts(opts))
			if tc.want == "" {
				if err != nil {
					t.Fatal(err)
				}
				if got := stackConfigValue(t, p.Directory, "insecureRegistries"); got != `["registry.local:5000"]` {
					t.Fatalf("insecureRegistries = %#v", got)
				}
				return
			}
			if err == nil || err.Error() != tc.want {
				t.Fatalf("error = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestPlatformConfigPassesBackingServiceClasses(t *testing.T) {
	classes := `[{"name":"egress-public","egress":{"allowedDestinations":["smtp.example.com:587"]}}]`
	p, opts := objectKeyTestPulumi(t)
	writePlatformConfig(t, &p, `{"backingServiceClasses":`+classes+`,"seedDefaultBackingClasses":false}`)
	if err := p.Up(context.Background(), opts); err != nil {
		t.Fatal(err)
	}
	p.ConfigFile = ""
	if err := p.Up(context.Background(), opts); err != nil {
		t.Fatal(err)
	}
	if got, want := stackConfigValue(t, p.Directory, "backingServiceClasses"), decodeJSON(t, classes); !reflect.DeepEqual(got, want) {
		t.Fatalf("backingServiceClasses = %v, want %v", got, want)
	}
	if got := stackConfigValue(t, p.Directory, "seedDefaultBackingClasses"); got != false {
		t.Fatalf("seedDefaultBackingClasses = %#v", got)
	}
	for body, want := range map[string]string{
		`{"backingServiceClasses":{"name":"x"}}`: "platform config backingServiceClasses must be a JSON array of class objects, got an object",
		`{"backingServiceClasses":["x"]}`:        "platform config backingServiceClasses[0] must be a JSON object, got a string",
		`{"seedDefaultBackingClasses":"false"}`:  "platform config seedDefaultBackingClasses must be a boolean, got a string",
	} {
		q, qopts := objectKeyTestPulumi(t)
		writePlatformConfig(t, &q, body)
		if _, err := q.prepare(qopts); err == nil || err.Error() != want {
			t.Fatalf("%s: error = %v, want %q", body, err, want)
		}
	}
}

const oldHostDigest = "sha256:ee89fd4bce4f9f35f4cd09c63d3cbdd07bea3071b5d372f82c9f49b9741c3669"

func TestPreviousDefaultImageFollowsPlatformPackage(t *testing.T) {
	// A CLI-managed image (unset, the current default, or an earlier default) becomes the
	// newest CLI default the pinned release lists for NATS credentials; a user-supplied
	// image is left alone. Below 6.0.20 no release lists any, and a file: tarball's version
	// is unknown: an unset image gets the current default and an earlier one is kept.
	wasiTLS := "ghcr.io/di-framework/wash:2.8.0-wasi-tls@"
	pre0002, pre0003, v6028 := wasiTLS+oldHostDigest, wasiTLS+pre0003HostDigest, wasiTLS+platform6028HostDigest
	replaced := func(from, to string) string {
		return "notice: tenantHostImage " + from + " is an earlier di-framework-kube default; replacing it with " + to + ", which rolls every tenant host\n"
	}
	downgraded := func(pkg, to string) string {
		return "notice: tenantHostImage " + DefaultTenantHostImage + " is the di-framework-kube default, which --platform-package " + pkg + " does not list for NATS credentials; replacing it with " + to + ", the newest default it lists, which rolls every tenant host\n"
	}
	kept := func(image, pkg, reason, listing string) string {
		return "notice: keeping tenantHostImage " + image + ", an earlier di-framework-kube default, because --platform-package " + pkg + " " + reason + "; " + listing + ". Set tenantHostImage in --platform-config to choose another image.\n"
	}
	const predates = "predates platform 6.0.20, the first release that lists a di-framework-kube default host image for NATS credentials"
	const unlisted = "no platform release lists it for NATS credentials, so a tenant registry stays anonymous on it"
	anonymous := func(pkg string) string {
		return "notice: --platform-package " + pkg + " lists no di-framework-kube default host image for NATS credentials, so a tenant registry stays anonymous on tenantHostImage " + DefaultTenantHostImage + " unless tenantHostImage in --platform-config names an image it lists\n"
	}
	const pkg6028, pkg6020, pkg6013, pkg6029rc = "@di-framework/platform@6.0.28", "@di-framework/platform@6.0.20", "@di-framework/platform@6.0.13", "@di-framework/platform@6.0.29-rc.1"
	tarball := "file:" + filepath.Join(t.TempDir(), "di-framework-platform-6.0.29.tgz")
	if err := os.WriteFile(strings.TrimPrefix(tarball, "file:"), []byte("build"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, pkg, image, wantImage, wantNotice string
	}{
		{"default package, 6.0.28 default", "", v6028, DefaultTenantHostImage, replaced(v6028, DefaultTenantHostImage)},
		{"6.0.29, 6.0.28 default", DefaultPlatformPackage, v6028, DefaultTenantHostImage, replaced(v6028, DefaultTenantHostImage)},
		{"6.1.0, pre-0003 default", "@di-framework/platform@6.1.0", pre0003, DefaultTenantHostImage, replaced(pre0003, DefaultTenantHostImage)},
		{"6.0.28, 6.0.28 default", pkg6028, v6028, v6028, ""},
		{"6.0.28, pre-0002 default", pkg6028, pre0002, v6028, replaced(pre0002, v6028)},
		{"6.0.28, pre-0003 default", pkg6028, pre0003, v6028, replaced(pre0003, v6028)},
		{"6.0.20, pre-0003 default", pkg6020, pre0003, v6028, replaced(pre0003, v6028)},
		{"6.0.28, current default", pkg6028, DefaultTenantHostImage, v6028, downgraded(pkg6028, v6028)},
		{"6.0.28, unset", pkg6028, "", v6028, ""},
		{"6.0.29 pre-release, 6.0.28 default", pkg6029rc, v6028, v6028, ""},
		{"6.0.19, unset", "@di-framework/platform@6.0.19", "", DefaultTenantHostImage, anonymous("@di-framework/platform@6.0.19")},
		{"6.0.13, pre-0003 default", pkg6013, pre0003, pre0003, kept(pre0003, pkg6013, predates, unlisted)},
		{"6.0.13, 6.0.28 default", pkg6013, v6028, v6028, kept(v6028, pkg6013, predates, "platform 6.0.20 and later list it for NATS credentials")},
		{"6.0.13, current default", pkg6013, DefaultTenantHostImage, DefaultTenantHostImage, anonymous(pkg6013)},
		{"tarball, unset", tarball, "", DefaultTenantHostImage, ""},
		{"tarball, 6.0.28 default", tarball, v6028, v6028, kept(v6028, tarball, "has no known platform version", "platform 6.0.20 and later list it for NATS credentials")},
		{"6.0.28, user image", pkg6028, "example/wash:local", "example/wash:local", ""},
	} {
		for _, source := range []string{"platform.json", "saved"} {
			if tc.image == "" && source == "platform.json" {
				continue
			}
			t.Run(tc.name+" "+source, func(t *testing.T) {
				p, opts := objectKeyTestPulumi(t)
				p.Package = tc.pkg
				var stderr bytes.Buffer
				p.Stderr = &stderr
				switch {
				case tc.image == "":
				case source == "saved":
					if err := os.WriteFile(filepath.Join(p.Directory, "Pulumi.dev.yaml"), []byte(`{"config":{"di-framework-kube:tenantHostImage":"`+tc.image+`"}}`), 0600); err != nil {
						t.Fatal(err)
					}
				default:
					writePlatformConfig(t, &p, `{"tenantHostImage":"`+tc.image+`"}`)
				}
				if _, err := p.prepare(opts); err != nil {
					t.Fatal(err)
				}
				if stderr.String() != tc.wantNotice {
					t.Fatalf("stderr = %q, want %q", stderr.String(), tc.wantNotice)
				}
				if got := stackConfigValue(t, p.Directory, "tenantHostImage"); got != tc.wantImage {
					t.Fatalf("tenantHostImage = %v, want %s", got, tc.wantImage)
				}
				// With the image saved, the next up has nothing to change and says nothing,
				// except that a kept earlier default and an unlisted current default are
				// announced on every up; a platform.json still naming an earlier default is
				// announced again too.
				stderr.Reset()
				if _, err := p.prepare(opts); err != nil {
					t.Fatal(err)
				}
				repeats := strings.HasPrefix(tc.wantNotice, "notice: keeping") || strings.HasPrefix(tc.wantNotice, "notice: --platform-package")
				if source == "saved" && !repeats && stderr.Len() != 0 {
					t.Fatalf("second up printed %q", stderr.String())
				}
				if source == "saved" && repeats && stderr.String() != tc.wantNotice {
					t.Fatalf("second up printed %q, want %q", stderr.String(), tc.wantNotice)
				}
			})
		}
	}
}

// pre0003HostDigest is the CLI's default from v0.0.5 (platform 6.0.13): patch 0002 only.
const pre0003HostDigest = "sha256:d88158b0b09edd9fc3e79d787762697c60f65baf134d895529e85d6c47cd80ad"

// platform6028HostDigest is the CLI's default on main after #16 (platform 6.0.28): patch
// 0002 and 0003.
const platform6028HostDigest = "sha256:8f0ee9c5a19ae0433e83a553431f1728ddf0a4787059c10e511209263d294537"

func TestDefaultTenantHostImageSupportsAllowlist(t *testing.T) {
	if preAllowlistHostImage(DefaultTenantHostImage) {
		t.Fatalf("DefaultTenantHostImage %s is on the platform's pre-0002 list", DefaultTenantHostImage)
	}
	if !strings.HasPrefix(DefaultTenantHostImage, "ghcr.io/di-framework/wash:2.8.0-wasi-tls@sha256:") {
		t.Fatal(DefaultTenantHostImage)
	}
	if slices.Contains(previousTenantHostImages, DefaultTenantHostImage) {
		t.Fatal("the current default is listed as a previous default")
	}
	// Each earlier default is a published wasi-tls build of this CLI's, listed once, so a
	// saved copy is replaced on the next up; the pre-0002 one stays on the platform's list.
	for i, image := range previousTenantHostImages {
		if !strings.HasPrefix(image, "ghcr.io/di-framework/wash:2.8.0-wasi-tls@sha256:") {
			t.Fatalf("previous default %s is not a published wasi-tls image; check whether it belongs here", image)
		}
		if slices.Index(previousTenantHostImages, image) != i {
			t.Fatalf("previous default %s is listed twice", image)
		}
	}
	for _, digest := range []string{oldHostDigest, pre0003HostDigest, platform6028HostDigest} {
		if !slices.Contains(previousTenantHostImages, "ghcr.io/di-framework/wash:2.8.0-wasi-tls@"+digest) {
			t.Fatalf("previous defaults %v lack %s", previousTenantHostImages, digest)
		}
	}
	if !preAllowlistHostImage("ghcr.io/di-framework/wash:2.8.0-wasi-tls@" + oldHostDigest) {
		t.Fatal("the pre-0002 default left the platform's pre-0002 list")
	}
}

func TestReplacesSavedPre0002DefaultImage(t *testing.T) {
	// Instances created by earlier CLI releases saved the ee89fd4b default.
	dir := t.TempDir()
	saved := "{\"config\":{\"di-framework-kube:tenantHostImage\":\"ghcr.io/di-framework/wash:2.8.0-wasi-tls@" + oldHostDigest + "\"}}\n"
	if err := os.WriteFile(filepath.Join(dir, "Pulumi.dev.yaml"), []byte(saved), 0600); err != nil {
		t.Fatal(err)
	}
	p := Pulumi{Directory: dir, Kubeconfig: "/tmp/kube", Namespace: "wasmcloud"}
	if _, err := p.prepare(InstallOptions{Release: "wasmcloud"}); err != nil {
		t.Fatal(err)
	}
	if got := stackConfigValue(t, dir, "tenantHostImage"); got != DefaultTenantHostImage {
		t.Fatalf("tenantHostImage = %v, want %s", got, DefaultTenantHostImage)
	}
}

func TestWarnsPre0002HostImageWithInsecureRegistries(t *testing.T) {
	for _, tc := range []struct {
		name, config string
		warn         bool
	}{
		{"default image", `{"insecureRegistries":` + insecureRegistriesJSON + `}`, false},
		{"explicit default image", `{"tenantHostImage":"` + DefaultTenantHostImage + `","insecureRegistries":` + insecureRegistriesJSON + `}`, false},
		// The old CLI defaults are replaced by the current one, so they do not warn.
		{"old CLI default", `{"tenantHostImage":"ghcr.io/di-framework/wash:2.8.0-wasi-tls@` + oldHostDigest + `","insecureRegistries":` + insecureRegistriesJSON + `}`, false},
		{"pre-0003 CLI default", `{"tenantHostImage":"ghcr.io/di-framework/wash:2.8.0-wasi-tls@` + pre0003HostDigest + `","insecureRegistries":` + insecureRegistriesJSON + `}`, false},
		{"6.0.28 CLI default", `{"tenantHostImage":"ghcr.io/di-framework/wash:2.8.0-wasi-tls@` + platform6028HostDigest + `","insecureRegistries":` + insecureRegistriesJSON + `}`, false},
		// The platform's rule is a digest suffix, whatever the repository or tag.
		{"mirrored pre-0002 digest", `{"tenantHostImage":"registry.local:5000/wash@` + oldHostDigest + `","insecureRegistries":` + insecureRegistriesJSON + `}`, true},
		{"retagged pre-0002 digest", `{"tenantHostImage":"ghcr.io/di-framework/wash:2.8.0@` + oldHostDigest + `","insecureRegistries":` + insecureRegistriesJSON + `}`, true},
		{"digest without @", `{"tenantHostImage":"registry.local/wash-` + oldHostDigest + `","insecureRegistries":` + insecureRegistriesJSON + `}`, false},
		{"tag only", `{"tenantHostImage":"example/wash:local","insecureRegistries":` + insecureRegistriesJSON + `}`, false},
		{"pre-0002 digest, empty list", `{"tenantHostImage":"registry.local:5000/wash@` + oldHostDigest + `","insecureRegistries":[]}`, false},
		{"pre-0002 digest, no list", `{"tenantHostImage":"registry.local:5000/wash@` + oldHostDigest + `"}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, opts := objectKeyTestPulumi(t)
			var stderr bytes.Buffer
			p.Stderr = &stderr
			writePlatformConfig(t, &p, tc.config)
			// Non-fatal: up proceeds either way.
			if err := p.Up(context.Background(), insecureOpts(opts)); err != nil {
				t.Fatal(err)
			}
			got := stderr.String()
			if tc.warn != strings.Contains(got, "warning: insecureRegistries is set and tenantHostImage") {
				t.Fatalf("warn=%v, stderr=%q", tc.warn, got)
			}
			if tc.warn && (!strings.Contains(got, "6.0.11") || !strings.Contains(got, "patch 0002") || !strings.Contains(got, oldHostDigest) || !strings.Contains(got, DefaultTenantHostImage)) {
				t.Fatalf("warning lacks version, patch, image or default: %q", got)
			}
		})
	}
	// A kept list and a kept pre-0002 image warn too.
	p, opts := objectKeyTestPulumi(t)
	var stderr bytes.Buffer
	p.Stderr = &stderr
	stack := handSetInsecureRegistriesYAML + "  di-framework-kube:tenantHostImage: registry.local:5000/wash@" + oldHostDigest + "\n"
	if err := os.WriteFile(filepath.Join(p.Directory, "Pulumi.dev.yaml"), []byte(stack), 0600); err != nil {
		t.Fatal(err)
	}
	if err := p.Up(context.Background(), insecureOpts(opts)); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stderr.String(), "warning: insecureRegistries is set") {
		t.Fatalf("no warning for a kept list and image: %q", stderr.String())
	}
}
