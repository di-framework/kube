package platform

import "testing"

func TestMergePreservesNestedDefaults(t *testing.T) {
	values := merge(map[string]any{"runtime": map[string]any{"resources": map[string]any{"requests": map[string]any{"cpu": "250m"}, "limits": map[string]any{"memory": "2Gi"}}}}, map[string]any{"runtime": map[string]any{"resources": map[string]any{"limits": map[string]any{"memory": "768Mi"}}}})
	resources := values["runtime"].(map[string]any)["resources"].(map[string]any)
	if resources["requests"].(map[string]any)["cpu"] != "250m" || resources["limits"].(map[string]any)["memory"] != "768Mi" {
		t.Fatal(values)
	}
}
