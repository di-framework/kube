package platform

import (
	"fmt"
	"io"
	"os"
	"time"

	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/cli"
	"helm.sh/helm/v3/pkg/registry"
	"helm.sh/helm/v3/pkg/release"
)

const (
	DefaultChart        = "oci://ghcr.io/wasmcloud/charts/runtime-operator"
	DefaultChartVersion = "2.8.0"
)

type Client struct {
	Kubeconfig string
	Context    string
	Namespace  string
	Debug      bool
	Stderr     io.Writer
}

type InstallOptions struct {
	Release                 string
	Chart                   string
	ChartVersion            string
	Timeout                 time.Duration
	NodePort                int
	AllowInsecureRegistries bool
	ValueFiles              []string
}

type Status struct {
	Name        string `json:"name"`
	Namespace   string `json:"namespace"`
	Status      string `json:"status"`
	Revision    int    `json:"revision"`
	Description string `json:"description,omitempty"`
}

func (c Client) Status(releaseName string) (Status, error) {
	configuration, _, _, err := c.configuration()
	if err != nil {
		return Status{}, err
	}
	result, err := action.NewStatus(configuration).Run(releaseName)
	if err != nil {
		return Status{}, fmt.Errorf("get Helm release %q: %w", releaseName, err)
	}
	return statusFromRelease(result), nil
}

func (c Client) Uninstall(releaseName string, timeout time.Duration) error {
	configuration, _, _, err := c.configuration()
	if err != nil {
		return err
	}
	uninstall := action.NewUninstall(configuration)
	uninstall.Wait = true
	uninstall.Timeout = timeout
	uninstall.IgnoreNotFound = true
	if _, err := uninstall.Run(releaseName); err != nil {
		return fmt.Errorf("uninstall Helm release %q: %w", releaseName, err)
	}
	return nil
}

func (c Client) configuration() (*action.Configuration, *cli.EnvSettings, *registry.Client, error) {
	settings := cli.New()
	settings.KubeConfig = c.Kubeconfig
	settings.KubeContext = c.Context
	configuration := new(action.Configuration)
	log := func(format string, values ...any) {
		if c.Debug {
			fmt.Fprintf(writerOrDiscard(c.Stderr), "helm: "+format+"\n", values...)
		}
	}
	if err := configuration.Init(settings.RESTClientGetter(), c.Namespace, os.Getenv("HELM_DRIVER"), log); err != nil {
		return nil, nil, nil, fmt.Errorf("initialize Helm: %w", err)
	}
	registryClient, err := registry.NewClient(
		registry.ClientOptDebug(c.Debug),
		registry.ClientOptWriter(writerOrDiscard(c.Stderr)),
	)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("initialize Helm registry client: %w", err)
	}
	configuration.RegistryClient = registryClient
	return configuration, settings, registryClient, nil
}

func merge(base, override map[string]any) map[string]any {
	result := make(map[string]any, len(base)+len(override))
	for key, value := range base {
		result[key] = value
	}
	for key, value := range override {
		if child, ok := value.(map[string]any); ok {
			if current, ok := result[key].(map[string]any); ok {
				result[key] = merge(current, child)
				continue
			}
		}
		result[key] = value
	}
	return result
}

func statusFromRelease(value *release.Release) Status {
	result := Status{Name: value.Name, Namespace: value.Namespace, Revision: value.Version}
	if value.Info != nil {
		result.Status = value.Info.Status.String()
		result.Description = value.Info.Description
	}
	return result
}

func writerOrDiscard(writer io.Writer) io.Writer {
	if writer == nil {
		return io.Discard
	}
	return writer
}
