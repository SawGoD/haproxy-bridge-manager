package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAllPathsConfigurableAndRoundTrip(t *testing.T) {
	dir := t.TempDir()
	want := Settings{
		HAProxyConfig:   filepath.Join(dir, "haproxy.cfg"),
		BackupDirectory: filepath.Join(dir, "backups"),
		StateDirectory:  filepath.Join(dir, "state"),
		SocketPath:      filepath.Join(dir, "bridge.sock"),
		PIDPath:         filepath.Join(dir, "bridge.pid"),
		LogPath:         filepath.Join(dir, "bridge.log"),
		BackupRetention: 23,
	}
	data, err := want.TOML()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "settings.toml")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("want %+v, got %+v", want, got)
	}
}

func TestInvalidTOMLRejected(t *testing.T) {
	inputs := []string{
		"HAProxyConfig = \"x\"",
		"[other]\nhaproxy_config = \"x\"\n",
		"[app]\nunknown = \"x\"\n",
		"[app]\nhaproxy_config = \"\"\n",
		"[app]\nbackup_retention = -1\n",
		"[app]\nbackup_retention = nope\n",
		"[app]\nlog_path = \"a\"\nlog_path = \"b\"\n",
	}
	for _, input := range inputs {
		if _, err := Parse([]byte(input)); err == nil {
			t.Errorf("accepted %q", input)
		}
	}
}
