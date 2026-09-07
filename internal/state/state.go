package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"time"
)

var validName = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,51}[a-z0-9])?$`)

// State is the durable connection contract for one managed platform instance.
// It intentionally contains no credentials; those stay in the mode-0600
// kubeconfig referenced by Kubeconfig.
type State struct {
	Name             string    `json:"name"`
	ManagedCluster   bool      `json:"managedCluster"`
	RunMode          string    `json:"runMode,omitempty"`
	KubesoloVersion  string    `json:"kubesoloVersion,omitempty"`
	Kubeconfig       string    `json:"kubeconfig"`
	Context          string    `json:"context,omitempty"`
	Namespace        string    `json:"namespace"`
	Release          string    `json:"release"`
	ChartVersion     string    `json:"chartVersion"`
	HTTPPort         int       `json:"httpPort,omitempty"`
	NodePort         int       `json:"nodePort,omitempty"`
	KubernetesServer string    `json:"kubernetesServer,omitempty"`
	UpdatedAt        time.Time `json:"updatedAt"`
}

type Store struct {
	Dir string
}

func DefaultDir() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("find user config directory: %w", err)
	}
	return filepath.Join(dir, "di-framework-kube"), nil
}

func ValidateName(name string) error {
	if !validName.MatchString(name) {
		return fmt.Errorf("invalid instance name %q: use 1-53 lowercase letters, digits, or hyphens", name)
	}
	return nil
}

func (s Store) InstanceDir(name string) (string, error) {
	if err := ValidateName(name); err != nil {
		return "", err
	}
	return filepath.Join(s.Dir, name), nil
}

func (s Store) KubeconfigPath(name string) (string, error) {
	dir, err := s.InstanceDir(name)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "kubeconfig"), nil
}

func (s Store) statePath(name string) (string, error) {
	dir, err := s.InstanceDir(name)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "state.json"), nil
}

func (s Store) Load(name string) (State, error) {
	path, err := s.statePath(name)
	if err != nil {
		return State{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return State{}, fmt.Errorf("instance %q has not been created", name)
		}
		return State{}, fmt.Errorf("read state: %w", err)
	}
	var result State
	if err := json.Unmarshal(data, &result); err != nil {
		return State{}, fmt.Errorf("decode state %s: %w", path, err)
	}
	return result, nil
}

func (s Store) Save(value State) error {
	path, err := s.statePath(value.Name)
	if err != nil {
		return err
	}
	value.UpdatedAt = time.Now().UTC()
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("encode state: %w", err)
	}
	data = append(data, '\n')
	return writeAtomic(path, data, 0o600)
}

func (s Store) SaveKubeconfig(name string, data []byte) (string, error) {
	path, err := s.KubeconfigPath(name)
	if err != nil {
		return "", err
	}
	if err := writeAtomic(path, data, 0o600); err != nil {
		return "", err
	}
	return path, nil
}

func (s Store) Remove(name string) error {
	dir, err := s.InstanceDir(name)
	if err != nil {
		return err
	}
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("remove state directory: %w", err)
	}
	return nil
}

func writeAtomic(path string, data []byte, mode fs.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create state directory: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return fmt.Errorf("create temporary file: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return fmt.Errorf("set temporary file permissions: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write temporary file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("sync temporary file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temporary file: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("replace %s: %w", path, err)
	}
	return nil
}
