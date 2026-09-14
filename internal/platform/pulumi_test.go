package platform

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
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
