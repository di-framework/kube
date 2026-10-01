package platform

import (
	"context"
	"fmt"

	core "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

const ownerConfigMap = "di-framework-platform-owner"

func (p Pulumi) claim(ctx context.Context, identity projectIdentity, release bool) error {
	if p.ownership != nil {
		return p.ownership(ctx, identity, release)
	}
	rules := &clientcmd.ClientConfigLoadingRules{ExplicitPath: identity.Kubeconfig}
	overrides := &clientcmd.ConfigOverrides{CurrentContext: identity.Context}
	config, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(rules, overrides).ClientConfig()
	if err != nil {
		return err
	}
	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		return err
	}
	return p.claimWithClient(ctx, client, identity, release)
}

func (p Pulumi) claimWithClient(ctx context.Context, client kubernetes.Interface, identity projectIdentity, release bool) error {
	maps := client.CoreV1().ConfigMaps("kube-system")
	existing, err := maps.Get(ctx, ownerConfigMap, meta.GetOptions{})
	if apierrors.IsNotFound(err) {
		if release {
			return nil
		}
		// Never implicitly adopt a Helm release or another platform's cluster-scoped CRDs.
		releases, err := client.CoreV1().Secrets(identity.Namespace).List(ctx, meta.ListOptions{LabelSelector: "owner=helm,name=" + identity.Release})
		if err != nil {
			return err
		}
		if len(releases.Items) > 0 {
			return fmt.Errorf("existing Helm release %s/%s is not owned by this Pulumi project; preserve its data and remove/migrate the legacy platform explicitly before retrying", identity.Namespace, identity.Release)
		}
		resources, err := client.Discovery().ServerResourcesForGroupVersion("platform.di-framework.dev/v1alpha1")
		if err == nil && len(resources.APIResources) > 0 {
			return fmt.Errorf("cluster already has DI Framework platform CRDs; use the owning Pulumi project instead of creating a competing installation")
		}
		if err != nil && !apierrors.IsNotFound(err) {
			return fmt.Errorf("check existing platform CRDs: %w", err)
		}
		_, err = maps.Create(ctx, &core.ConfigMap{ObjectMeta: meta.ObjectMeta{Name: ownerConfigMap}, Data: map[string]string{"owner": identity.Owner, "project": p.Directory}}, meta.CreateOptions{})
		if apierrors.IsAlreadyExists(err) {
			return fmt.Errorf("another platform installation claimed the cluster; retry using its owning project")
		}
		return err
	}
	if err != nil {
		return err
	}
	if existing.Data["owner"] != identity.Owner {
		return fmt.Errorf("cluster platform is owned by another Pulumi project (%s)", existing.Data["project"])
	}
	if release {
		return maps.Delete(ctx, ownerConfigMap, meta.DeleteOptions{Preconditions: &meta.Preconditions{UID: &existing.UID, ResourceVersion: &existing.ResourceVersion}})
	}
	return nil
}
