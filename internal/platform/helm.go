package platform

import (
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/cli"
	"helm.sh/helm/v3/pkg/registry"
	"helm.sh/helm/v3/pkg/release"
	"helm.sh/helm/v3/pkg/storage/driver"
	"sigs.k8s.io/yaml"
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

func (c Client) InstallOrUpgrade(options InstallOptions) (Status, error) {
	configuration, settings, registryClient, err := c.configuration()
	if err != nil {
		return Status{}, err
	}
	loadedChart, err := loadChart(configuration, settings, registryClient, options.Chart, options.ChartVersion)
	if err != nil {
		return Status{}, err
	}
	values, err := chartValues(options)
	if err != nil {
		return Status{}, err
	}

	get := action.NewGet(configuration)
	existing, getErr := get.Run(options.Release)
	var result *release.Release
	if getErr == nil && existing != nil {
		upgrade := action.NewUpgrade(configuration)
		upgrade.Namespace = c.Namespace
		upgrade.Atomic = true
		upgrade.CleanupOnFail = true
		upgrade.Wait = true
		upgrade.Timeout = options.Timeout
		upgrade.MaxHistory = 5
		result, err = upgrade.Run(options.Release, loadedChart, values)
	} else if errors.Is(getErr, driver.ErrReleaseNotFound) {
		install := action.NewInstall(configuration)
		install.ReleaseName = options.Release
		install.Namespace = c.Namespace
		install.CreateNamespace = true
		install.Atomic = true
		install.Wait = true
		install.Timeout = options.Timeout
		result, err = install.Run(loadedChart, values)
	} else {
		return Status{}, fmt.Errorf("look up Helm release %q: %w", options.Release, getErr)
	}
	if err != nil {
		return Status{}, fmt.Errorf("install wasmCloud release %q: %w", options.Release, err)
	}
	return statusFromRelease(result), nil
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

func loadChart(configuration *action.Configuration, settings *cli.EnvSettings, registryClient *registry.Client, reference, version string) (*chart.Chart, error) {
	downloader := action.NewInstall(configuration)
	downloader.ChartPathOptions.Version = version
	downloader.SetRegistryClient(registryClient)
	path, err := downloader.ChartPathOptions.LocateChart(reference, settings)
	if err != nil {
		return nil, fmt.Errorf("download wasmCloud chart %s@%s: %w", reference, version, err)
	}
	loaded, err := loader.Load(path)
	if err != nil {
		return nil, fmt.Errorf("load wasmCloud chart: %w", err)
	}
	if err := action.CheckDependencies(loaded, loaded.Metadata.Dependencies); err != nil {
		return nil, fmt.Errorf("check wasmCloud chart dependencies: %w", err)
	}
	return loaded, nil
}

func chartValues(options InstallOptions) (map[string]any, error) {
	service := map[string]any{"type": "ClusterIP"}
	httpConfig := map[string]any{"enabled": true, "port": 9191}
	if options.NodePort > 0 {
		service["type"] = "NodePort"
		httpConfig["nodePort"] = options.NodePort
	}
	runtime := map[string]any{
		"resources": map[string]any{
			"requests":          map[string]any{"cpu": "250m", "memory": "256Mi"},
			"limits":            map[string]any{"memory": "2Gi"},
			"defaultHeapMemory": "512MiB",
			"coreInstances":     "100",
		},
		"hostGroups": []any{map[string]any{
			"name":     "default",
			"replicas": 1,
			"service":  service,
			"webgpu":   map[string]any{"enabled": false},
			"http":     httpConfig,
		}},
	}
	if options.AllowInsecureRegistries {
		runtime["extraArgs"] = []any{"--allow-insecure-registries"}
	}
	values := map[string]any{
		"gateway": map[string]any{"enabled": false},
		"runtime": runtime,
	}
	for _, file := range options.ValueFiles {
		data, err := os.ReadFile(file)
		if err != nil {
			return nil, fmt.Errorf("read values file %s: %w", file, err)
		}
		var override map[string]any
		if err := yaml.Unmarshal(data, &override); err != nil {
			return nil, fmt.Errorf("decode values file %s: %w", file, err)
		}
		values = merge(values, override)
	}
	return values, nil
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
