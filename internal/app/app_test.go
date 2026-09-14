package app

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestValidateUpOptions(t *testing.T) {
	t.Parallel()
	valid := upOptions{
		name:            "local",
		runMode:         "container",
		namespace:       "wasmcloud",
		release:         "wasmcloud",
		chart:           "oci://example/chart",
		chartVersion:    "1.0.0",
		timeout:         time.Minute,
		httpPort:        28080,
		nodePort:        30080,
		kubesoloVersion: "v1.2.0",
	}
	if err := validateUpOptions(valid); err != nil {
		t.Fatalf("valid options rejected: %v", err)
	}

	tests := []struct {
		name   string
		change func(*upOptions)
		want   string
	}{
		{"bad name", func(value *upOptions) { value.name = "Bad_Name" }, "invalid instance name"},
		{"bad mode", func(value *upOptions) { value.runMode = "daemon" }, "invalid --run-mode"},
		{"bad node port", func(value *upOptions) { value.nodePort = 8080 }, "NodePort range"},
		{"missing node port", func(value *upOptions) { value.nodePort = 0 }, "requires a non-zero --node-port"},
		{"bad timeout", func(value *upOptions) { value.timeout = 0 }, "--timeout"},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			value := valid
			test.change(&value)
			err := validateUpOptions(value)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want substring %q", err, test.want)
			}
		})
	}
}

func TestVersionJSON(t *testing.T) {
	t.Parallel()
	var stdout, stderr bytes.Buffer
	root := newRoot(rootOptions{
		stateDir: t.TempDir(),
		stdout:   &stdout,
		stderr:   &stderr,
		build:    BuildInfo{Version: "1.2.3", Commit: "abc123", Date: "2026-09-07"},
	})
	root.SetArgs([]string{"version", "--output", "json"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"version": "1.2.3"`, `"commit": "abc123"`} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("output %q does not contain %q", stdout.String(), want)
		}
	}
}

func TestUpChecksToolsBeforeCreatingCluster(t *testing.T) {
	for _, missing := range []string{"pulumi", "npm", "node"} {
		t.Run(missing, func(t *testing.T) {
			bin := t.TempDir()
			for _, name := range []string{"pulumi", "npm", "node"} {
				if name != missing {
					if err := os.WriteFile(filepath.Join(bin, name), []byte("#!/bin/sh\nexit 97\n"), 0700); err != nil {
						t.Fatal(err)
					}
				}
			}
			t.Setenv("PATH", bin)
			dir := t.TempDir()
			var stdout, stderr bytes.Buffer
			root := newRoot(rootOptions{stateDir: dir, stdout: &stdout, stderr: &stderr})
			root.SetArgs([]string{"up"})
			err := root.Execute()
			if err == nil || !strings.Contains(err.Error(), "shared platform requires "+missing+" on PATH") {
				t.Fatalf("unexpected error: %v", err)
			}
			if !errors.Is(err, exec.ErrNotFound) {
				t.Fatalf("missing underlying lookup error: %v", err)
			}
			if strings.Contains(stdout.String(), "Ensuring Kubesolo") {
				t.Fatal("started provisioning before checking tools")
			}
			entries, err := os.ReadDir(dir)
			if err != nil {
				t.Fatal(err)
			}
			if len(entries) != 0 {
				t.Fatal("created instance files before checking tools")
			}
		})
	}
}
