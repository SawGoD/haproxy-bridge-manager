package settings

import (
	"path/filepath"
	"testing"
)

func TestRoundTrip(t *testing.T) {
	s := Default()
	p := filepath.Join(t.TempDir(), "config.toml")
	s.Docker.ContainerName = "custom-haproxy"
	if e := s.Save(p); e != nil {
		t.Fatal(e)
	}
	got, e := Load(p)
	if e != nil || got.Docker.ContainerName != s.Docker.ContainerName {
		t.Fatalf("%+v %v", got, e)
	}
}
func TestInvalid(t *testing.T) {
	s := Default()
	s.General.MaxBackups = 0
	if s.Validate() == nil {
		t.Fatal("zero retention accepted")
	}
	if _, e := Parse("[general]\nconfig_path = 123"); e == nil {
		t.Fatal("invalid TOML accepted")
	}
}
