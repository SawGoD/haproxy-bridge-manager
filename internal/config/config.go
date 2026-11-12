package config

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"strconv"
	"strings"
)

type Settings struct {
	HAProxyConfig   string
	BackupDirectory string
	StateDirectory  string
	SocketPath      string
	PIDPath         string
	LogPath         string
	BackupRetention int
}

func Default() Settings {
	return Settings{
		HAProxyConfig:   "/etc/haproxy/haproxy.cfg",
		BackupDirectory: "/var/lib/haproxy-bridge/backups",
		StateDirectory:  "/var/lib/haproxy-bridge",
		SocketPath:      "/run/haproxy-bridge.sock",
		PIDPath:         "/run/haproxy-bridge.pid",
		LogPath:         "/var/log/haproxy-bridge.log",
		BackupRetention: 10,
	}
}

func Load(path string) (Settings, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Settings{}, err
	}
	return Parse(data)
}

func Parse(data []byte) (Settings, error) {
	settings := Default()
	scanner := bufio.NewScanner(bytes.NewReader(data))
	section := ""
	seen := make(map[string]struct{})
	for lineNumber := 1; scanner.Scan(); lineNumber++ {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = strings.TrimSpace(line[1 : len(line)-1])
			if section != "app" {
				return Settings{}, fmt.Errorf("config: line %d: unknown section %q", lineNumber, section)
			}
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok || section != "app" {
			return Settings{}, fmt.Errorf("config: line %d: expected [app] key = value", lineNumber)
		}
		key = strings.TrimSpace(key)
		if _, ok := seen[key]; ok {
			return Settings{}, fmt.Errorf("config: line %d: duplicate key %q", lineNumber, key)
		}
		seen[key] = struct{}{}
		value = strings.TrimSpace(value)
		if key == "backup_retention" {
			retention, err := strconv.Atoi(value)
			if err != nil {
				return Settings{}, fmt.Errorf("config: line %d: invalid retention", lineNumber)
			}
			settings.BackupRetention = retention
			continue
		}
		path, err := strconv.Unquote(value)
		if err != nil {
			return Settings{}, fmt.Errorf("config: line %d: invalid string: %w", lineNumber, err)
		}
		switch key {
		case "haproxy_config":
			settings.HAProxyConfig = path
		case "backup_directory":
			settings.BackupDirectory = path
		case "state_directory":
			settings.StateDirectory = path
		case "socket_path":
			settings.SocketPath = path
		case "pid_path":
			settings.PIDPath = path
		case "log_path":
			settings.LogPath = path
		default:
			return Settings{}, fmt.Errorf("config: line %d: unknown key %q", lineNumber, key)
		}
	}
	if err := scanner.Err(); err != nil {
		return Settings{}, err
	}
	return settings, settings.Validate()
}

func (s Settings) Validate() error {
	paths := map[string]string{
		"haproxy_config":   s.HAProxyConfig,
		"backup_directory": s.BackupDirectory,
		"state_directory":  s.StateDirectory,
		"socket_path":      s.SocketPath,
		"pid_path":         s.PIDPath,
		"log_path":         s.LogPath,
	}
	for name, value := range paths {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("config: %s is required", name)
		}
	}
	if s.BackupRetention < 0 {
		return fmt.Errorf("config: backup_retention cannot be negative")
	}
	return nil
}

func (s Settings) TOML() ([]byte, error) {
	if err := s.Validate(); err != nil {
		return nil, err
	}
	return []byte(fmt.Sprintf(`[app]
haproxy_config = %s
backup_directory = %s
state_directory = %s
socket_path = %s
pid_path = %s
log_path = %s
backup_retention = %d
`, strconv.Quote(s.HAProxyConfig), strconv.Quote(s.BackupDirectory), strconv.Quote(s.StateDirectory), strconv.Quote(s.SocketPath), strconv.Quote(s.PIDPath), strconv.Quote(s.LogPath), s.BackupRetention)), nil
}
