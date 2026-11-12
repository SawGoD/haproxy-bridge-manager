// Package platform isolates process execution and HAProxy lifecycle operations.
package platform

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type Runner interface {
	Run(context.Context, string, ...string) ([]byte, error)
}
type ExecRunner struct{}

func (ExecRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	b, e := exec.CommandContext(ctx, name, args...).CombinedOutput()
	if e != nil {
		return b, fmt.Errorf("%s: %w\n%s", name, e, b)
	}
	return b, nil
}

type Mount struct {
	Type, Source, Destination, Name string
	RW                              bool
}
type Container struct {
	Image  string
	State  struct{ Running bool }
	Config struct {
		Image, User, WorkingDir string
		Env, Cmd, Entrypoint    []string
	}
	Mounts     []Mount
	HostConfig struct {
		NetworkMode string
		Privileged  bool
	}
}
type Docker struct {
	Runner                                  Runner
	ContainerName, ComposeFile, ServiceName string
}

func safe(s string) error {
	if s == "" || strings.HasPrefix(s, "-") || strings.ContainsAny(s, "\x00\r\n") {
		return errors.New("empty or unsafe process argument")
	}
	return nil
}
func (d Docker) Inspect(ctx context.Context) (Container, error) {
	var c []Container
	if e := safe(d.ContainerName); e != nil {
		return Container{}, e
	}
	b, e := d.Runner.Run(ctx, "docker", "inspect", "--type", "container", d.ContainerName)
	if e != nil {
		return Container{}, e
	}
	if e = json.Unmarshal(b, &c); e != nil {
		return Container{}, e
	}
	if len(c) != 1 {
		return Container{}, errors.New("container not found")
	}
	return c[0], nil
}
func configMount(c Container, host string) (string, bool, error) {
	host, e := filepath.Abs(host)
	if e != nil {
		return "", false, e
	}
	best := ""
	dest := ""
	single := false
	for _, m := range c.Mounts {
		if m.Type != "bind" {
			continue
		}
		src := filepath.Clean(m.Source)
		if host == src || strings.HasPrefix(host, src+string(os.PathSeparator)) {
			if len(src) > len(best) {
				best = src
				rel, _ := filepath.Rel(src, host)
				dest = filepath.ToSlash(filepath.Join(m.Destination, rel))
				single = host == src
			}
		}
	}
	if best == "" {
		return "", false, errors.New("config must be covered by a verified host bind mount")
	}
	return dest, single, nil
}

// Validate substitutes a read-only candidate into the actual image and mount layout.
// Unrecognized startup semantics are rejected rather than silently validated differently.
func (d Docker) Validate(ctx context.Context, host, candidate string) error {
	c, e := d.Inspect(ctx)
	if e != nil {
		return e
	}
	dest, _, e := configMount(c, host)
	if e != nil {
		return e
	}
	if c.Image == "" {
		return errors.New("missing immutable image ID")
	}
	if len(c.Config.Entrypoint) > 1 || len(c.Config.Entrypoint) == 1 && !strings.HasSuffix(c.Config.Entrypoint[0], "docker-entrypoint.sh") {
		return errors.New("custom entrypoint validation is unsupported")
	}
	cmd := c.Config.Cmd
	if len(cmd) > 0 && cmd[0] == "haproxy" {
		cmd = cmd[1:]
	}
	found := false
	extra := []string{}
	for i := 0; i < len(cmd); i++ {
		switch cmd[i] {
		case "-f":
			if i+1 >= len(cmd) {
				return errors.New("missing -f argument")
			}
			i++
			if cmd[i] != dest {
				return errors.New("multiple or directory config inputs require manual validation support")
			}
			found = true
		case "-W", "-db", "-D":
		case "-C", "-L":
			if i+1 >= len(cmd) {
				return errors.New("missing startup argument")
			}
			extra = append(extra, cmd[i], cmd[i+1])
			i++
		default:
			return fmt.Errorf("unsupported HAProxy startup argument %q", cmd[i])
		}
	}
	if !found {
		return errors.New("managed config is not the container startup -f input")
	}
	a := []string{"run", "--rm", "--network", "none", "--entrypoint", "haproxy"}
	if c.Config.User != "" {
		a = append(a, "--user", c.Config.User)
	}
	if c.Config.WorkingDir != "" {
		a = append(a, "--workdir", c.Config.WorkingDir)
	}
	for _, v := range c.Config.Env {
		a = append(a, "--env", v)
	}
	for _, m := range c.Mounts {
		if m.Type != "bind" && m.Type != "volume" {
			return errors.New("unsupported mount type")
		}
		if strings.ContainsAny(m.Source+m.Destination, ",\n\r") {
			return errors.New("unsupported mount path")
		}
		if m.Type == "volume" {
			if m.Name == "" {
				return errors.New("volume name unavailable")
			}
			m.Source = m.Name
		}
		if m.Destination == dest {
			continue
		}
		a = append(a, "--mount", "type="+m.Type+",src="+m.Source+",dst="+m.Destination+",readonly")
	}
	abs, e := filepath.Abs(candidate)
	if e != nil {
		return e
	}
	if strings.ContainsAny(abs, ",\n\r") {
		return errors.New("unsupported candidate path")
	}
	a = append(a, "--mount", "type=bind,src="+abs+",dst="+dest+",readonly", c.Image, "-c")
	a = append(a, extra...)
	a = append(a, "-f", dest)
	_, e = d.Runner.Run(ctx, "docker", a...)
	return e
}
func (d Docker) Visible(ctx context.Context, host string) error {
	c, e := d.Inspect(ctx)
	if e != nil {
		return e
	}
	dest, _, e := configMount(c, host)
	if e != nil {
		return e
	}
	b, e := d.Runner.Run(ctx, "docker", "exec", d.ContainerName, "cat", dest)
	if e != nil {
		return e
	}
	local, e := os.ReadFile(host)
	if e != nil {
		return e
	}
	if sha256.Sum256(local) != sha256.Sum256(b) {
		return errors.New("container sees stale config inode; use compose recreate")
	}
	return nil
}
func (d Docker) Apply(ctx context.Context, host, method string) error {
	if method == "manual" {
		return errors.New("manual mode: configuration has not been applied")
	}
	c, e := d.Inspect(ctx)
	if e != nil {
		return e
	}
	_, single, e := configMount(c, host)
	if e != nil {
		return e
	}
	switch method {
	case "restart":
		if single {
			return errors.New("single-file bind requires recreate after atomic save")
		}
		if e = d.Visible(ctx, host); e != nil {
			return e
		}
		_, e = d.Runner.Run(ctx, "docker", "restart", d.ContainerName)
	case "recreate":
		if e = safe(d.ComposeFile); e != nil {
			return e
		}
		if e = safe(d.ServiceName); e != nil {
			return e
		}
		_, e = d.Runner.Run(ctx, "docker", "compose", "-f", d.ComposeFile, "up", "-d", "--no-deps", "--force-recreate", d.ServiceName)
	case "graceful":
		e = d.graceful(ctx, host, c)
	default:
		return errors.New("unsupported apply method")
	}
	if e != nil {
		return e
	}
	c, e = d.Inspect(ctx)
	if e != nil {
		return e
	}
	if !c.State.Running {
		return errors.New("container is not running after apply")
	}
	return d.Visible(ctx, host)
}
func (d Docker) Logs(ctx context.Context) (string, error) {
	if e := safe(d.ContainerName); e != nil {
		return "", e
	}
	b, e := d.Runner.Run(ctx, "docker", "logs", "--tail", "100", d.ContainerName)
	return string(b), e
}

type Native struct {
	Runner          Runner
	Binary, Service string
}

func (n Native) Validate(ctx context.Context, path string) error {
	if e := safe(n.Binary); e != nil {
		return e
	}
	_, e := n.Runner.Run(ctx, n.Binary, "-c", "-f", path)
	return e
}
func (n Native) Apply(ctx context.Context, method string) error {
	if e := safe(n.Service); e != nil {
		return e
	}
	if method != "restart" {
		return errors.New("only verified native restart is supported")
	}
	if _, e := n.Runner.Run(ctx, "systemctl", "restart", n.Service); e != nil {
		return e
	}
	_, e := n.Runner.Run(ctx, "systemctl", "is-active", "--quiet", n.Service)
	return e
}

func (d Docker) graceful(ctx context.Context, host string, c Container) error {
	if !c.State.Running {
		return errors.New("container not running")
	}
	if e := d.Visible(ctx, host); e != nil {
		return e
	}
	if len(c.Config.Entrypoint) > 1 || len(c.Config.Entrypoint) == 1 && !strings.HasSuffix(c.Config.Entrypoint[0], "docker-entrypoint.sh") {
		return errors.New("custom entrypoint: graceful capability unknown")
	}
	cmd, e := d.Runner.Run(ctx, "docker", "exec", d.ContainerName, "cat", "/proc/1/cmdline")
	if e != nil {
		return e
	}
	argv := strings.Split(string(cmd), "\x00")
	if len(argv) == 0 || filepath.Base(argv[0]) != "haproxy" {
		return errors.New("PID 1 is not HAProxy")
	}
	master := false
	for _, a := range argv {
		if a == "-W" || a == "-Ws" {
			master = true
		}
	}
	if !master {
		return errors.New("PID 1 master-worker mode not verified")
	}
	before, e := d.Runner.Run(ctx, "docker", "top", d.ContainerName, "-eo", "pid,ppid,args")
	if e != nil {
		return e
	}
	pids := processIDs(string(before))
	if len(pids) < 2 {
		return errors.New("master and worker not confirmed")
	}
	if _, e = d.Runner.Run(ctx, "docker", "kill", "--signal", "USR2", d.ContainerName); e != nil {
		return e
	}
	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("new worker not confirmed: %w", ctx.Err())
		case <-time.After(100 * time.Millisecond):
		}
		after, e := d.Runner.Run(ctx, "docker", "top", d.ContainerName, "-eo", "pid,ppid,args")
		if e != nil {
			return e
		}
		for pid := range processIDs(string(after)) {
			if !pids[pid] {
				return nil
			}
		}
	}
}
func processIDs(s string) map[string]bool {
	out := map[string]bool{}
	for _, line := range strings.Split(s, "\n")[1:] {
		f := strings.Fields(line)
		if len(f) >= 3 && strings.Contains(strings.Join(f[2:], " "), "haproxy") {
			out[f[0]] = true
		}
	}
	return out
}
