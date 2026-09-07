package app

import (
	"bytes"
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
