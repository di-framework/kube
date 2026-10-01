package platform

import (
	"context"
	"strings"
	"testing"

	core "k8s.io/api/core/v1"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestClaimRefusesAnotherOwner(t *testing.T) {
	client := fake.NewClientset(&core.ConfigMap{ObjectMeta: meta.ObjectMeta{Name: ownerConfigMap, Namespace: "kube-system"}, Data: map[string]string{"owner": "other", "project": "/another/project"}})
	p := Pulumi{Directory: "/this/project"}
	for _, release := range []bool{false, true} {
		err := p.claimWithClient(context.Background(), client, projectIdentity{Owner: "ours"}, release)
		if err == nil || !strings.Contains(err.Error(), "another Pulumi project") {
			t.Fatal(err)
		}
	}
	if _, err := client.CoreV1().ConfigMaps("kube-system").Get(context.Background(), ownerConfigMap, meta.GetOptions{}); err != nil {
		t.Fatal(err)
	}
}

func TestClaimRefusesUnmanagedHelmInstallation(t *testing.T) {
	client := fake.NewClientset(&core.Secret{ObjectMeta: meta.ObjectMeta{Name: "sh.helm.release.v1.wasmcloud.v1", Namespace: "wasmcloud", Labels: map[string]string{"owner": "helm", "name": "wasmcloud"}}})
	err := (Pulumi{}).claimWithClient(context.Background(), client, projectIdentity{Namespace: "wasmcloud", Release: "wasmcloud", Owner: "ours"}, false)
	if err == nil || !strings.Contains(err.Error(), "existing Helm release") {
		t.Fatal(err)
	}
	maps, _ := client.CoreV1().ConfigMaps("kube-system").List(context.Background(), meta.ListOptions{})
	if len(maps.Items) != 0 {
		t.Fatal("claimed legacy installation")
	}
}

func TestClaimAndReleaseOwnedCluster(t *testing.T) {
	client := fake.NewClientset()
	p := Pulumi{Directory: "/our/project"}
	identity := projectIdentity{Namespace: "wasmcloud", Release: "wasmcloud", Owner: "ours"}
	if err := p.claimWithClient(context.Background(), client, identity, false); err != nil {
		t.Fatal(err)
	}
	if err := p.claimWithClient(context.Background(), client, identity, false); err != nil {
		t.Fatal(err)
	}
	if err := p.claimWithClient(context.Background(), client, identity, true); err != nil {
		t.Fatal(err)
	}
	maps, _ := client.CoreV1().ConfigMaps("kube-system").List(context.Background(), meta.ListOptions{})
	if len(maps.Items) != 0 {
		t.Fatal("claim not released")
	}
}
