package platform

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type fake struct {
	calls            [][]string
	fail             string
	inspect, content string
}

func (f *fake) Run(_ context.Context, n string, a ...string) ([]byte, error) {
	f.calls = append(f.calls, append([]string{n}, a...))
	if strings.Contains(strings.Join(a, " "), f.fail) && f.fail != "" {
		return nil, errors.New("injected failure")
	}
	if a[0] == "inspect" {
		return []byte(f.inspect), nil
	}
	if a[0] == "exec" {
		return []byte(f.content), nil
	}
	return nil, nil
}
func fixture(host string, single bool) string {
	src := filepath.Dir(host)
	dst := "/cfg"
	if single {
		src = host
		dst = "/cfg/haproxy.cfg"
	}
	return `[{"Image":"sha256:actual","State":{"Running":true},"Config":{"Cmd":["haproxy","-f","/cfg/haproxy.cfg"]},"Mounts":[{"Type":"bind","Source":"` + src + `","Destination":"` + dst + `"}]}]`
}
func TestDockerFailures(t *testing.T) {
	ctx := context.Background()
	for _, fail := range []string{"inspect", "restart", "up -d"} {
		t.Run(fail, func(t *testing.T) {
			p := filepath.Join(t.TempDir(), "haproxy.cfg")
			os.WriteFile(p, []byte("x"), 0600)
			f := &fake{fail: fail, inspect: fixture(p, false), content: "x"}
			d := Docker{f, "haproxy", "/tmp/compose.yaml", "haproxy"}
			method := "restart"
			if fail == "up -d" {
				method = "recreate"
			}
			if d.Apply(ctx, p, method) == nil {
				t.Fatal("expected failure")
			}
		})
	}
}
func TestDockerCandidateAndBind(t *testing.T) {
	p := filepath.Join(t.TempDir(), "haproxy.cfg")
	candidate := filepath.Join(filepath.Dir(p), "candidate")
	f := &fake{inspect: fixture(p, true)}
	d := Docker{f, "haproxy", "/tmp/compose.yaml", "haproxy"}
	if e := d.Validate(context.Background(), p, candidate); e != nil {
		t.Fatal(e)
	}
	args := strings.Join(f.calls[len(f.calls)-1], " ")
	if !strings.Contains(args, "src="+candidate) || !strings.Contains(args, "sha256:actual -c -f /cfg/haproxy.cfg") {
		t.Fatal(args)
	}
	if e := d.Apply(context.Background(), p, "restart"); e == nil {
		t.Fatal("stale inode restart allowed")
	}
}
func TestUnknownContainerAndUnsafeName(t *testing.T) {
	for _, name := range []string{"haproxy", "--help"} {
		f := &fake{inspect: "[]"}
		d := Docker{Runner: f, ContainerName: name}
		if _, e := d.Inspect(context.Background()); e == nil {
			t.Fatal("expected error")
		}
	}
}
func TestStaleContent(t *testing.T) {
	p := filepath.Join(t.TempDir(), "haproxy.cfg")
	os.WriteFile(p, []byte("new"), 0600)
	f := &fake{inspect: fixture(p, false), content: "old"}
	d := Docker{Runner: f, ContainerName: "haproxy"}
	if d.Visible(context.Background(), p) == nil {
		t.Fatal("stale content accepted")
	}
}

type gracefulFake struct {
	fake
	top    int
	master bool
}

func (f *gracefulFake) Run(ctx context.Context, n string, a ...string) ([]byte, error) {
	if a[0] == "exec" && a[len(a)-1] == "/proc/1/cmdline" {
		if f.master {
			return []byte("haproxy\x00-W\x00-db\x00-f\x00/cfg/haproxy.cfg\x00"), nil
		}
		return []byte("sh\x00entrypoint\x00"), nil
	}
	if a[0] == "top" {
		f.top++
		if f.top > 1 {
			return []byte("PID PPID COMMAND\n1 0 haproxy -W\n3 1 haproxy -W\n"), nil
		}
		return []byte("PID PPID COMMAND\n1 0 haproxy -W\n2 1 haproxy -W\n"), nil
	}
	return f.fake.Run(ctx, n, a...)
}
func TestGracefulRequiresVerifiedMasterAndNewWorker(t *testing.T) {
	p := filepath.Join(t.TempDir(), "haproxy.cfg")
	os.WriteFile(p, []byte("x"), 0600)
	for _, master := range []bool{false, true} {
		f := &gracefulFake{fake: fake{inspect: fixture(p, false), content: "x"}, master: master}
		d := Docker{Runner: f, ContainerName: "haproxy"}
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		e := d.Apply(ctx, p, "graceful")
		cancel()
		if master && e != nil {
			t.Fatal(e)
		}
		if !master && e == nil {
			t.Fatal("unverified master signaled")
		}
	}
}
