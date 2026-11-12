package integration

import (
	"context"
	"fmt"
	"github.com/SawGoD/haproxy-bridge/internal/platform"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// These tests use only freshly allocated temporary directories and containers.
func TestHAProxy32(t *testing.T) {
	if os.Getenv("HAPROXY_BRIDGE_INTEGRATION") != "1" {
		t.Skip("set HAPROXY_BRIDGE_INTEGRATION=1 to run isolated Docker tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	run := func(a ...string) (string, error) {
		b, e := exec.CommandContext(ctx, "docker", a...).CombinedOutput()
		return string(b), e
	}
	if b, e := run("version"); e != nil {
		t.Fatalf("Docker required: %v %s", e, b)
	}
	dir := t.TempDir()
	cfg := filepath.Join(dir, "haproxy.cfg")
	good := "global\n    maxconn 100\ndefaults\n    mode tcp\n    timeout connect 1s\n    timeout client 1s\n    timeout server 1s\nfrontend in\n    bind 127.0.0.1:10443\n    default_backend out\nbackend out\n    server s1 127.0.0.1:443 check\n"
	if e := os.WriteFile(cfg, []byte(good), 0644); e != nil {
		t.Fatal(e)
	}
	args := []string{"run", "--rm", "--network", "none", "--mount", "type=bind,src=" + cfg + ",dst=/cfg/haproxy.cfg,readonly", "haproxy:3.2", "haproxy", "-c", "-f", "/cfg/haproxy.cfg"}
	if b, e := run(args...); e != nil {
		t.Fatalf("valid config rejected %v %s", e, b)
	}
	if e := os.WriteFile(cfg, []byte(good+"    definitely-invalid-option yes\n"), 0644); e != nil {
		t.Fatal(e)
	}
	if b, e := run(args...); e == nil {
		t.Fatalf("invalid config accepted %s", b)
	}
	if e := os.WriteFile(cfg, []byte(good), 0644); e != nil {
		t.Fatal(e)
	}
	name := "hbm-test-" + strings.ToLower(filepath.Base(dir))
	b, e := run("run", "-d", "--network", "none", "--name", name, "--mount", "type=bind,src="+cfg+",dst=/cfg/haproxy.cfg,readonly", "haproxy:3.2", "haproxy", "-f", "/cfg/haproxy.cfg")
	if e != nil {
		t.Fatalf("isolated start %v %s", e, b)
	}
	t.Cleanup(func() {
		c, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		exec.CommandContext(c, "docker", "rm", "-f", name).Run()
	})
	tmp := filepath.Join(dir, "candidate")
	newContent := good + "# candidate inode\n"
	os.WriteFile(tmp, []byte(newContent), 0644)
	if e = os.Rename(tmp, cfg); e != nil {
		t.Fatal(e)
	}
	b, e = run("exec", name, "cat", "/cfg/haproxy.cfg")
	if e != nil {
		t.Fatal(e, b)
	}
	if b != good {
		t.Fatalf("expected old inode content, got %q", b)
	}
}

func TestManagedDocker32(t *testing.T) {
	if os.Getenv("HAPROXY_BRIDGE_INTEGRATION") != "1" {
		t.Skip("explicit integration mode required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	runner := platform.ExecRunner{}
	dir := t.TempDir()
	config := filepath.Join(dir, "haproxy.cfg")
	good := []byte("global\n maxconn 100\ndefaults\n mode tcp\n timeout connect 1s\n timeout client 1s\n timeout server 1s\nfrontend f\n bind 127.0.0.1:10443\n default_backend b\nbackend b\n server s 127.0.0.1:443 check\n")
	os.WriteFile(config, good, 0644)
	name := fmt.Sprintf("hbm-managed-%d", time.Now().UnixNano())
	compose := filepath.Join(dir, "compose.yaml")
	content := fmt.Sprintf("services:\n  bridge:\n    image: haproxy:3.2\n    container_name: %s\n    network_mode: none\n    command: [haproxy, -f, /cfg/haproxy.cfg]\n    volumes:\n      - %s:/cfg/haproxy.cfg:ro\n", name, config)
	os.WriteFile(compose, []byte(content), 0600)
	if b, e := runner.Run(ctx, "docker", "compose", "-f", compose, "up", "-d", "--no-deps", "bridge"); e != nil {
		t.Fatalf("start %v %s", e, b)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		runner.Run(ctx, "docker", "compose", "-f", compose, "down")
	})
	d := platform.Docker{Runner: runner, ContainerName: name, ComposeFile: compose, ServiceName: "bridge"}
	candidate := filepath.Join(dir, "candidate.cfg")
	os.WriteFile(candidate, good, 0644)
	if e := d.Validate(ctx, config, candidate); e != nil {
		t.Fatalf("candidate %v", e)
	}
	os.WriteFile(candidate, append(append([]byte{}, good...), []byte(" invalid-directive yes\n")...), 0644)
	if d.Validate(ctx, config, candidate) == nil {
		t.Fatal("invalid candidate accepted")
	}
	next := append(append([]byte{}, good...), []byte("# replacement\n")...)
	os.WriteFile(candidate, next, 0644)
	os.Rename(candidate, config)
	if d.Visible(ctx, config) == nil {
		t.Fatal("stale bind undetected")
	}
	if d.Apply(ctx, config, "restart") == nil {
		t.Fatal("single-file restart permitted")
	}
	if e := d.Apply(ctx, config, "recreate"); e != nil {
		t.Fatal(e)
	}
	if e := d.Apply(ctx, config, "graceful"); e != nil {
		t.Fatalf("graceful %v", e)
	}
}
