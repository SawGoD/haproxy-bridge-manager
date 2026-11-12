package settings

import (
	"fmt"
	"github.com/BurntSushi/toml"
	"os"
	"path/filepath"
	"strings"
)

type Settings struct {
	Names   map[string]string `toml:"node_names"`
	General struct {
		ConfigPath string `toml:"config_path"`
		BackupDir  string `toml:"backup_dir"`
		AutoBackup bool   `toml:"auto_backup"`
		MaxBackups int    `toml:"max_backups"`
	} `toml:"general"`
	HAProxy struct {
		Mode              string `toml:"mode"`
		ValidationEnabled bool   `toml:"validation_enabled"`
		ValidationTimeout int    `toml:"validation_timeout_seconds"`
	} `toml:"haproxy"`
	Docker struct {
		ContainerName string `toml:"container_name"`
		ComposeFile   string `toml:"compose_file"`
		ServiceName   string `toml:"service_name"`
		ApplyMethod   string `toml:"apply_method"`
	} `toml:"docker"`
	Native struct {
		Binary      string `toml:"binary"`
		ServiceName string `toml:"service_name"`
		ApplyMethod string `toml:"apply_method"`
	} `toml:"native"`
	Network struct {
		DefaultBindAddress string `toml:"default_bind_address"`
		DefaultStartPort   int    `toml:"default_start_port"`
		DefaultTargetPort  int    `toml:"default_target_port"`
		ConnectTimeout     int    `toml:"connect_timeout_seconds"`
		SNI                string `toml:"sni"`
	} `toml:"network"`
	Healthcheck struct {
		Enabled  bool `toml:"enabled"`
		Interval int  `toml:"interval_seconds"`
		Fall     int  `toml:"fall"`
		Rise     int  `toml:"rise"`
	} `toml:"healthcheck"`
	Monitoring struct {
		Socket string `toml:"runtime_socket"`
	} `toml:"monitoring"`
	UI struct {
		RefreshInterval int  `toml:"refresh_interval_seconds"`
		Confirm         bool `toml:"confirm_before_apply"`
		Advanced        bool `toml:"show_advanced_options"`
	} `toml:"ui"`
}

func Default() Settings {
	var s Settings
	s.Names = map[string]string{}
	s.General.ConfigPath = "/opt/haproxy/haproxy.cfg"
	s.General.BackupDir = "/opt/haproxy/backups"
	s.General.AutoBackup = true
	s.General.MaxBackups = 30
	s.HAProxy.Mode = "docker"
	s.HAProxy.ValidationEnabled = true
	s.HAProxy.ValidationTimeout = 15
	s.Docker.ContainerName = "haproxy"
	s.Docker.ComposeFile = "/opt/haproxy/compose.yaml"
	s.Docker.ServiceName = "haproxy"
	s.Docker.ApplyMethod = "restart"
	s.Native.Binary = "haproxy"
	s.Native.ServiceName = "haproxy"
	s.Native.ApplyMethod = "restart"
	s.Network.DefaultBindAddress = "127.0.0.1"
	s.Network.DefaultStartPort = 10443
	s.Network.DefaultTargetPort = 443
	s.Network.ConnectTimeout = 5
	s.Healthcheck.Enabled = true
	s.Healthcheck.Interval = 3
	s.Healthcheck.Fall = 2
	s.Healthcheck.Rise = 2
	s.UI.RefreshInterval = 3
	s.UI.Confirm = true
	return s
}
func Path() (string, error) {
	home, e := os.UserHomeDir()
	if e != nil {
		return "", e
	}
	return filepath.Join(home, ".config", "haproxy-bridge", "config.toml"), nil
}
func Load(p string) (Settings, error) {
	s := Default()
	_, e := toml.DecodeFile(p, &s)
	if e != nil {
		return s, e
	}
	return s, s.Validate()
}
func Parse(b string) (Settings, error) {
	s := Default()
	_, e := toml.Decode(b, &s)
	if e != nil {
		return s, e
	}
	return s, s.Validate()
}
func (s Settings) String() string {
	var b strings.Builder
	toml.NewEncoder(&b).Encode(s)
	return b.String()
}
func (s Settings) Validate() error {
	if s.HAProxy.Mode != "native" && s.HAProxy.Mode != "docker" {
		return fmt.Errorf("mode must be native or docker")
	}
	if !filepath.IsAbs(s.General.ConfigPath) || !filepath.IsAbs(s.General.BackupDir) {
		return fmt.Errorf("config and backup paths must be absolute")
	}
	if s.General.MaxBackups < 1 || s.Network.ConnectTimeout < 1 || s.HAProxy.ValidationTimeout < 1 || s.UI.RefreshInterval < 1 || s.Healthcheck.Interval < 1 || s.Healthcheck.Fall < 1 || s.Healthcheck.Rise < 1 {
		return fmt.Errorf("timeouts, retention and check thresholds must be positive")
	}
	for _, p := range []int{s.Network.DefaultStartPort, s.Network.DefaultTargetPort} {
		if p < 1 || p > 65535 {
			return fmt.Errorf("invalid default port")
		}
	}
	return nil
}
func (s Settings) Save(p string) error {
	if e := s.Validate(); e != nil {
		return e
	}
	if e := os.MkdirAll(filepath.Dir(p), 0700); e != nil {
		return e
	}
	f, e := os.CreateTemp(filepath.Dir(p), ".settings-*")
	if e != nil {
		return e
	}
	defer os.Remove(f.Name())
	if _, e = f.WriteString(s.String()); e != nil {
		f.Close()
		return e
	}
	if e = f.Sync(); e != nil {
		f.Close()
		return e
	}
	if e = f.Close(); e != nil {
		return e
	}
	return os.Rename(f.Name(), p)
}
