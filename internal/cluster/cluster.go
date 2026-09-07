package cluster

import (
	"context"
	"fmt"
	"time"

	"k8s.io/client-go/discovery"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

func WaitReady(ctx context.Context, kubeconfig, kubeContext string, timeout time.Duration) error {
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	var lastErr error

	for {
		if err := Probe(kubeconfig, kubeContext); err == nil {
			return nil
		} else {
			lastErr = err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return fmt.Errorf("Kubernetes API was not ready after %s: %w", timeout, lastErr)
		case <-ticker.C:
		}
	}
}

func Probe(kubeconfig, kubeContext string) error {
	config, err := restConfig(kubeconfig, kubeContext)
	return probeConfig(config, err)
}

func ProbeData(kubeconfig []byte, kubeContext string) error {
	raw, err := clientcmd.Load(kubeconfig)
	if err != nil {
		return fmt.Errorf("load kubeconfig: %w", err)
	}
	overrides := &clientcmd.ConfigOverrides{}
	if kubeContext != "" {
		overrides.CurrentContext = kubeContext
	}
	config, err := clientcmd.NewDefaultClientConfig(*raw, overrides).ClientConfig()
	return probeConfig(config, err)
}

func probeConfig(config *rest.Config, err error) error {
	if err != nil {
		return fmt.Errorf("load Kubernetes client configuration: %w", err)
	}
	config.Timeout = 5 * time.Second
	client, err := discovery.NewDiscoveryClientForConfig(config)
	if err != nil {
		return fmt.Errorf("create Kubernetes discovery client: %w", err)
	}
	if _, err := client.ServerVersion(); err != nil {
		return fmt.Errorf("contact Kubernetes API: %w", err)
	}
	return nil
}

func Server(kubeconfig, kubeContext string) (string, error) {
	raw, err := clientcmd.LoadFromFile(kubeconfig)
	if err != nil {
		return "", fmt.Errorf("load kubeconfig: %w", err)
	}
	contextName := kubeContext
	if contextName == "" {
		contextName = raw.CurrentContext
	}
	contextConfig, ok := raw.Contexts[contextName]
	if !ok {
		return "", fmt.Errorf("kubeconfig context %q does not exist", contextName)
	}
	clusterConfig, ok := raw.Clusters[contextConfig.Cluster]
	if !ok {
		return "", fmt.Errorf("kubeconfig cluster %q does not exist", contextConfig.Cluster)
	}
	return clusterConfig.Server, nil
}

func restConfig(kubeconfig, kubeContext string) (*rest.Config, error) {
	rules := &clientcmd.ClientConfigLoadingRules{ExplicitPath: kubeconfig}
	overrides := &clientcmd.ConfigOverrides{}
	if kubeContext != "" {
		overrides.CurrentContext = kubeContext
	}
	config, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(rules, overrides).ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("load Kubernetes client configuration: %w", err)
	}
	return config, nil
}
