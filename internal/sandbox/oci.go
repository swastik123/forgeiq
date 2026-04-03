package sandbox

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Minimal OCI config.json generator for runsc.
// This intentionally avoids containerd/docker and runs runsc directly.

type ociConfig struct {
	OCIVersion string `json:"ociVersion"`
	Process    struct {
		Terminal bool     `json:"terminal"`
		User     struct{} `json:"user"`
		Args     []string `json:"args"`
		Env      []string `json:"env,omitempty"`
		Cwd      string   `json:"cwd"`
		Rlimits  []struct {
			Type string `json:"type"`
			Hard uint64 `json:"hard"`
			Soft uint64 `json:"soft"`
		} `json:"rlimits,omitempty"`
		NoNewPrivileges bool `json:"noNewPrivileges"`
	} `json:"process"`
	Root struct {
		Path     string `json:"path"`
		Readonly bool   `json:"readonly"`
	} `json:"root"`
	Mounts []struct {
		Destination string   `json:"destination"`
		Type        string   `json:"type"`
		Source      string   `json:"source"`
		Options     []string `json:"options,omitempty"`
	} `json:"mounts"`
	Linux struct {
		Resources struct {
			Memory *struct {
				Limit *int64 `json:"limit,omitempty"`
			} `json:"memory,omitempty"`
			CPU *struct {
				Quota  *int64 `json:"quota,omitempty"`
				Period *uint64 `json:"period,omitempty"`
			} `json:"cpu,omitempty"`
			Pids *struct {
				Limit int64 `json:"limit"`
			} `json:"pids,omitempty"`
		} `json:"resources,omitempty"`
	} `json:"linux,omitempty"`
}

type BundleSpec struct {
	BundleDir  string
	RootfsPath string // absolute path to rootfs directory
	Workspace  string // host path
	OutDir     string // host path
	VerifySh   string // host path to executable script to run inside container

	TimeoutSec int
	MemBytes   int64
	CPUMillis  int64
	Pids       int64
	NoFile     uint64
}

func WriteBundle(spec BundleSpec) error {
	if spec.BundleDir == "" || spec.RootfsPath == "" {
		return fmt.Errorf("bundle_dir and rootfs_path required")
	}
	if err := os.MkdirAll(spec.BundleDir, 0o755); err != nil {
		return err
	}
	// Rootfs is referenced relative to bundle dir.
	// We symlink the real rootfs into bundle/rootfs.
	link := filepath.Join(spec.BundleDir, "rootfs")
	_ = os.Remove(link)
	if err := os.Symlink(spec.RootfsPath, link); err != nil {
		return err
	}

	var cfg ociConfig
	cfg.OCIVersion = "1.0.2"
	cfg.Root.Path = "rootfs"
	cfg.Root.Readonly = true
	cfg.Process.Terminal = false
	cfg.Process.Args = []string{"/bin/bash", "/verify.sh"}
	cfg.Process.Cwd = "/workspace"
	cfg.Process.NoNewPrivileges = true
	if spec.NoFile > 0 {
		cfg.Process.Rlimits = append(cfg.Process.Rlimits, struct {
			Type string `json:"type"`
			Hard uint64 `json:"hard"`
			Soft uint64 `json:"soft"`
		}{Type: "RLIMIT_NOFILE", Hard: spec.NoFile, Soft: spec.NoFile})
	}

	// Mounts
	addMount := func(dst, typ, src string, opts ...string) {
		m := struct {
			Destination string   `json:"destination"`
			Type        string   `json:"type"`
			Source      string   `json:"source"`
			Options     []string `json:"options,omitempty"`
		}{Destination: dst, Type: typ, Source: src, Options: opts}
		cfg.Mounts = append(cfg.Mounts, m)
	}

	addMount("/proc", "proc", "proc")
	addMount("/tmp", "tmpfs", "tmpfs", "nosuid", "noexec", "nodev", "mode=1777")
	addMount("/workspace", "bind", spec.Workspace, "rbind", "rw")
	addMount("/out", "bind", spec.OutDir, "rbind", "rw")
	addMount("/verify.sh", "bind", spec.VerifySh, "bind", "ro")

	// Linux resources (best-effort; depends on host cgroup setup)
	if spec.Pids > 0 {
		cfg.Linux.Resources.Pids = &struct {
			Limit int64 `json:"limit"`
		}{Limit: spec.Pids}
	}
	if spec.MemBytes > 0 {
		cfg.Linux.Resources.Memory = &struct {
			Limit *int64 `json:"limit,omitempty"`
		}{Limit: &spec.MemBytes}
	}
	if spec.CPUMillis > 0 {
		// Quota = millicores * period / 1000. Use 100000us period.
		period := uint64(100000)
		quota := int64(spec.CPUMillis) * int64(period) / 1000
		cfg.Linux.Resources.CPU = &struct {
			Quota  *int64 `json:"quota,omitempty"`
			Period *uint64 `json:"period,omitempty"`
		}{Quota: &quota, Period: &period}
	}

	b, _ := json.MarshalIndent(cfg, "", "  ")
	return os.WriteFile(filepath.Join(spec.BundleDir, "config.json"), b, 0o644)
}

