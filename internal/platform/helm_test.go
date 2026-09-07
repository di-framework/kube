package platform

import (
	"os"
	"path/filepath"
	"testing"
)

func TestChartValuesKubesoloHTTPProfile(t *testing.T) {
	t.Parallel()
	values, err := chartValues(InstallOptions{NodePort: 30080, AllowInsecureRegistries: true})
	if err != nil {
		t.Fatal(err)
	}
	runtimeValues := values["runtime"].(map[string]any)
	groups := runtimeValues["hostGroups"].([]any)
	group := groups[0].(map[string]any)
	if got := group["service"].(map[string]any)["type"]; got != "NodePort" {
		t.Fatalf("service type = %v", got)
	}
	if got := group["http"].(map[string]any)["nodePort"]; got != 30080 {
		t.Fatalf("nodePort = %v", got)
	}
	if len(runtimeValues["extraArgs"].([]any)) != 1 {
		t.Fatal("insecure registry argument was not added")
	}
}

func TestChartValuesFileOverridesDefaults(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "values.yaml")
	if err := os.WriteFile(path, []byte("runtime:\n  resources:\n    limits:\n      memory: 768Mi\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	values, err := chartValues(InstallOptions{ValueFiles: []string{path}})
	if err != nil {
		t.Fatal(err)
	}
	resources := values["runtime"].(map[string]any)["resources"].(map[string]any)
	if got := resources["limits"].(map[string]any)["memory"]; got != "768Mi" {
		t.Fatalf("memory limit = %v", got)
	}
	if got := resources["requests"].(map[string]any)["cpu"]; got != "250m" {
		t.Fatalf("cpu request was not retained: %v", got)
	}
}
