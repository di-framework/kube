package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/di-framework/di-framework-kube/internal/cluster"
	"github.com/di-framework/di-framework-kube/internal/kubesolo"
	"github.com/di-framework/di-framework-kube/internal/platform"
	"github.com/di-framework/di-framework-kube/internal/state"
	"github.com/spf13/cobra"
)

type BuildInfo struct {
	Version string `json:"version"`
	Commit  string `json:"commit"`
	Date    string `json:"date"`
}

type upOptions struct {
	name                    string
	runMode                 string
	kubesoloVersion         string
	kubesoloctl             string
	dataPath                string
	kubeconfig              string
	kubeContext             string
	namespace               string
	release                 string
	chart                   string
	chartVersion            string
	timeout                 time.Duration
	httpPort                int
	nodePort                int
	allowInsecureRegistries bool
	valueFiles              []string
	debug                   bool
}

type rootOptions struct {
	stateDir string
	stdout   io.Writer
	stderr   io.Writer
	build    BuildInfo
}

func Execute(build BuildInfo) error {
	stateDir, err := state.DefaultDir()
	if err != nil {
		return err
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	root := newRoot(rootOptions{stateDir: stateDir, stdout: os.Stdout, stderr: os.Stderr, build: build})
	root.SetContext(ctx)
	return root.Execute()
}

func newRoot(options rootOptions) *cobra.Command {
	root := &cobra.Command{
		Use:           "di-framework-kube",
		Short:         "Run wasmCloud on a dedicated Kubesolo cluster",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.SetOut(options.stdout)
	root.SetErr(options.stderr)
	root.PersistentFlags().StringVar(&options.stateDir, "state-dir", options.stateDir, "directory for instance state and kubeconfigs")
	root.AddCommand(
		newUpCommand(&options),
		newDownCommand(&options),
		newStatusCommand(&options),
		newOutputsCommand(&options),
		newKubeconfigCommand(&options),
		newVersionCommand(&options),
	)
	return root
}

func newUpCommand(root *rootOptions) *cobra.Command {
	options := upOptions{}
	command := &cobra.Command{
		Use:   "up",
		Short: "Create Kubesolo and install or upgrade wasmCloud",
		RunE: func(command *cobra.Command, args []string) error {
			return runUp(command.Context(), command.OutOrStdout(), command.ErrOrStderr(), state.Store{Dir: root.stateDir}, options)
		},
	}
	flags := command.Flags()
	flags.StringVar(&options.name, "name", "local", "instance name")
	flags.StringVar(&options.runMode, "run-mode", "container", "Kubesolo run mode: container or service")
	flags.StringVar(&options.kubesoloVersion, "kubesolo-version", kubesolo.DefaultVersion, "Kubesolo version")
	flags.StringVar(&options.kubesoloctl, "kubesoloctl", "", "path to kubesoloctl (downloaded when omitted)")
	flags.StringVar(&options.dataPath, "kubesolo-data", "/var/lib/kubesolo", "Kubesolo data path in service mode")
	flags.StringVar(&options.kubeconfig, "kubeconfig", "", "use an existing cluster instead of creating Kubesolo")
	flags.StringVar(&options.kubeContext, "context", "", "kubeconfig context")
	flags.StringVar(&options.namespace, "namespace", "wasmcloud", "namespace for the wasmCloud platform")
	flags.StringVar(&options.release, "release", "wasmcloud", "Helm release name")
	flags.StringVar(&options.chart, "chart", platform.DefaultChart, "wasmCloud Helm chart")
	flags.StringVar(&options.chartVersion, "chart-version", platform.DefaultChartVersion, "wasmCloud chart version")
	flags.DurationVar(&options.timeout, "timeout", 10*time.Minute, "cluster and Helm readiness timeout")
	flags.IntVar(&options.httpPort, "http-port", 28080, "localhost port for wasmCloud HTTP in container mode (0 disables publishing)")
	flags.IntVar(&options.nodePort, "node-port", 30080, "Kubernetes NodePort for the wasmCloud host (0 uses ClusterIP)")
	flags.BoolVar(&options.allowInsecureRegistries, "allow-insecure-registries", false, "allow hosts to pull components from plain-HTTP OCI registries")
	flags.StringSliceVarP(&options.valueFiles, "values", "f", nil, "additional Helm values file (repeatable)")
	flags.BoolVar(&options.debug, "debug", false, "show Helm debug logs")
	return command
}

func runUp(ctx context.Context, stdout, stderr io.Writer, store state.Store, options upOptions) error {
	if err := validateUpOptions(options); err != nil {
		return err
	}

	managed := options.kubeconfig == ""
	kubeconfig := options.kubeconfig
	created := false
	storedRunMode := options.runMode
	storedHTTPPort := options.httpPort
	if managed {
		fmt.Fprintf(stdout, "Ensuring Kubesolo %s (%s mode)...\n", options.kubesoloVersion, options.runMode)
		manager := kubesolo.Manager{
			Version: options.kubesoloVersion,
			Binary:  options.kubesoloctl,
			Stdout:  stdout,
			Stderr:  stderr,
			Probe: func(data []byte) error {
				return cluster.ProbeData(data, "")
			},
		}
		data, wasCreated, err := manager.Ensure(ctx, kubesolo.Options{
			Name:     options.name,
			RunMode:  options.runMode,
			DataPath: options.dataPath,
			HTTPPort: options.httpPort,
			NodePort: options.nodePort,
		})
		if err != nil {
			return err
		}
		created = wasCreated
		kubeconfig, err = store.SaveKubeconfig(options.name, data)
		if err != nil {
			return fmt.Errorf("save Kubesolo kubeconfig: %w", err)
		}
	} else {
		storedRunMode = "external"
		storedHTTPPort = 0
		absolute, err := filepath.Abs(kubeconfig)
		if err != nil {
			return fmt.Errorf("resolve kubeconfig: %w", err)
		}
		kubeconfig = absolute
	}

	fmt.Fprintln(stdout, "Waiting for the Kubernetes API...")
	if err := cluster.WaitReady(ctx, kubeconfig, options.kubeContext, options.timeout); err != nil {
		return err
	}
	server, err := cluster.Server(kubeconfig, options.kubeContext)
	if err != nil {
		return err
	}

	client := platform.Client{
		Kubeconfig: kubeconfig,
		Context:    options.kubeContext,
		Namespace:  options.namespace,
		Debug:      options.debug,
		Stderr:     stderr,
	}
	fmt.Fprintf(stdout, "Installing wasmCloud %s...\n", options.chartVersion)
	releaseStatus, err := client.InstallOrUpgrade(platform.InstallOptions{
		Release:                 options.release,
		Chart:                   options.chart,
		ChartVersion:            options.chartVersion,
		Timeout:                 options.timeout,
		NodePort:                options.nodePort,
		AllowInsecureRegistries: options.allowInsecureRegistries,
		ValueFiles:              options.valueFiles,
	})
	if err != nil {
		return err
	}

	value := state.State{
		Name:             options.name,
		ManagedCluster:   managed,
		RunMode:          storedRunMode,
		KubesoloVersion:  options.kubesoloVersion,
		Kubeconfig:       kubeconfig,
		Context:          options.kubeContext,
		Namespace:        options.namespace,
		Release:          options.release,
		ChartVersion:     options.chartVersion,
		HTTPPort:         storedHTTPPort,
		NodePort:         options.nodePort,
		KubernetesServer: server,
	}
	if err := store.Save(value); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "wasmCloud is %s in namespace %s (revision %d).\n", releaseStatus.Status, releaseStatus.Namespace, releaseStatus.Revision)
	if managed && created {
		fmt.Fprintln(stdout, "A new Kubesolo cluster was created.")
	}
	if managed && options.httpPort > 0 && options.nodePort > 0 && options.runMode == "container" {
		fmt.Fprintf(stdout, "Workload HTTP entrypoint: http://127.0.0.1:%d (route with the workload Host header)\n", options.httpPort)
	} else if options.nodePort > 0 {
		fmt.Fprintf(stdout, "Workload HTTP NodePort: %d\n", options.nodePort)
	}
	return nil
}

func validateUpOptions(options upOptions) error {
	if err := state.ValidateName(options.name); err != nil {
		return err
	}
	if options.runMode != "container" && options.runMode != "service" {
		return fmt.Errorf("invalid --run-mode %q: use container or service", options.runMode)
	}
	if options.timeout <= 0 {
		return errors.New("--timeout must be greater than zero")
	}
	if options.httpPort < 0 || options.httpPort > 65535 {
		return errors.New("--http-port must be 0 or between 1 and 65535")
	}
	if options.nodePort != 0 && (options.nodePort < 30000 || options.nodePort > 32767) {
		return errors.New("--node-port must be 0 or within Kubernetes' 30000-32767 NodePort range")
	}
	if options.runMode == "container" && options.httpPort > 0 && options.nodePort == 0 && options.kubeconfig == "" {
		return errors.New("--http-port requires a non-zero --node-port in container mode")
	}
	for _, value := range []struct{ label, value string }{
		{"namespace", options.namespace}, {"release", options.release}, {"chart", options.chart}, {"chart version", options.chartVersion},
	} {
		if strings.TrimSpace(value.value) == "" {
			return fmt.Errorf("%s must not be empty", value.label)
		}
	}
	return nil
}

func newDownCommand(root *rootOptions) *cobra.Command {
	var name string
	var purgeCluster bool
	var kubesoloctl string
	var timeout time.Duration
	command := &cobra.Command{
		Use:   "down",
		Short: "Uninstall wasmCloud and optionally remove Kubesolo",
		RunE: func(command *cobra.Command, args []string) error {
			store := state.Store{Dir: root.stateDir}
			value, err := store.Load(name)
			if err != nil {
				return err
			}
			client := platform.Client{Kubeconfig: value.Kubeconfig, Context: value.Context, Namespace: value.Namespace, Stderr: command.ErrOrStderr()}
			fmt.Fprintf(command.OutOrStdout(), "Uninstalling wasmCloud release %s...\n", value.Release)
			if err := client.Uninstall(value.Release, timeout); err != nil {
				return err
			}
			if !purgeCluster {
				fmt.Fprintln(command.OutOrStdout(), "wasmCloud was removed; the Kubesolo cluster was preserved.")
				return nil
			}
			if !value.ManagedCluster {
				return errors.New("refusing to purge an externally supplied Kubernetes cluster")
			}
			manager := kubesolo.Manager{Version: value.KubesoloVersion, Binary: kubesoloctl, Stdout: command.OutOrStdout(), Stderr: command.ErrOrStderr()}
			if err := manager.Uninstall(command.Context(), kubesolo.Options{Name: value.Name, RunMode: value.RunMode}, true); err != nil {
				return err
			}
			if err := store.Remove(value.Name); err != nil {
				return err
			}
			fmt.Fprintln(command.OutOrStdout(), "wasmCloud, Kubesolo, and the managed cluster data were removed.")
			return nil
		},
	}
	command.Flags().StringVar(&name, "name", "local", "instance name")
	command.Flags().BoolVar(&purgeCluster, "purge-cluster", false, "also permanently delete the managed Kubesolo cluster and its data")
	command.Flags().StringVar(&kubesoloctl, "kubesoloctl", "", "path to kubesoloctl")
	command.Flags().DurationVar(&timeout, "timeout", 10*time.Minute, "uninstall timeout")
	return command
}

func newStatusCommand(root *rootOptions) *cobra.Command {
	var name, output string
	command := &cobra.Command{
		Use:   "status",
		Short: "Show Kubernetes and wasmCloud status",
		RunE: func(command *cobra.Command, args []string) error {
			value, err := (state.Store{Dir: root.stateDir}).Load(name)
			if err != nil {
				return err
			}
			if err := cluster.Probe(value.Kubeconfig, value.Context); err != nil {
				return fmt.Errorf("Kubernetes is unavailable: %w", err)
			}
			status, err := (platform.Client{Kubeconfig: value.Kubeconfig, Context: value.Context, Namespace: value.Namespace, Stderr: command.ErrOrStderr()}).Status(value.Release)
			if err != nil {
				return err
			}
			if output == "json" {
				return writeJSON(command.OutOrStdout(), status)
			}
			fmt.Fprintf(command.OutOrStdout(), "Kubernetes: ready (%s)\nwasmCloud: %s (%s, revision %d)\n", value.KubernetesServer, status.Status, status.Namespace, status.Revision)
			return nil
		},
	}
	command.Flags().StringVar(&name, "name", "local", "instance name")
	command.Flags().StringVarP(&output, "output", "o", "text", "output format: text or json")
	return command
}

type outputs struct {
	Kubeconfig string `json:"kubeconfig"`
	Context    string `json:"context,omitempty"`
	Namespace  string `json:"namespace"`
	Endpoints  struct {
		HTTP       string `json:"http,omitempty"`
		Kubernetes string `json:"kubernetes"`
	} `json:"endpoints"`
}

func newOutputsCommand(root *rootOptions) *cobra.Command {
	var name string
	command := &cobra.Command{
		Use:   "outputs",
		Short: "Print the platform connection contract as JSON",
		RunE: func(command *cobra.Command, args []string) error {
			value, err := (state.Store{Dir: root.stateDir}).Load(name)
			if err != nil {
				return err
			}
			result := outputs{Kubeconfig: value.Kubeconfig, Context: value.Context, Namespace: value.Namespace}
			result.Endpoints.Kubernetes = value.KubernetesServer
			if value.HTTPPort > 0 && value.RunMode == "container" {
				result.Endpoints.HTTP = fmt.Sprintf("http://127.0.0.1:%d", value.HTTPPort)
			}
			return writeJSON(command.OutOrStdout(), result)
		},
	}
	command.Flags().StringVar(&name, "name", "local", "instance name")
	return command
}

func newKubeconfigCommand(root *rootOptions) *cobra.Command {
	var name string
	command := &cobra.Command{
		Use:   "kubeconfig",
		Short: "Print the instance kubeconfig path",
		RunE: func(command *cobra.Command, args []string) error {
			value, err := (state.Store{Dir: root.stateDir}).Load(name)
			if err != nil {
				return err
			}
			fmt.Fprintln(command.OutOrStdout(), value.Kubeconfig)
			return nil
		},
	}
	command.Flags().StringVar(&name, "name", "local", "instance name")
	return command
}

func newVersionCommand(root *rootOptions) *cobra.Command {
	var output string
	command := &cobra.Command{
		Use:   "version",
		Short: "Print build information",
		RunE: func(command *cobra.Command, args []string) error {
			if output == "json" {
				return writeJSON(command.OutOrStdout(), root.build)
			}
			fmt.Fprintf(command.OutOrStdout(), "di-framework-kube %s (%s, %s)\n", root.build.Version, root.build.Commit, root.build.Date)
			return nil
		},
	}
	command.Flags().StringVarP(&output, "output", "o", "text", "output format: text or json")
	return command
}

func writeJSON(writer io.Writer, value any) error {
	encoder := json.NewEncoder(writer)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}
