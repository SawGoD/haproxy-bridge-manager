package haproxy

import (
	"strings"
	"testing"
)

func TestReorderPreservesOptions(t *testing.T) {
	d, e := Parse([]byte("backend b\n server s1 127.0.0.1:443 check send-proxy # first\n # preserve\n server s2 127.0.0.2:443 backup check\n"))
	if e != nil {
		t.Fatal(e)
	}
	if e = d.ReorderServers("b", []string{"s2", "s1"}); e != nil {
		t.Fatal(e)
	}
	b := string(d.Bytes())
	if strings.Index(b, "server s2") > strings.Index(b, "server s1") || !strings.Contains(b, "send-proxy # first") || !strings.Contains(b, "# preserve") {
		t.Fatal(b)
	}
}
func TestConditionalMutationBlocked(t *testing.T) {
	d, e := Parse([]byte(".if defined(FOO)\nbackend b\n server s 127.0.0.1:443\n.endif\n"))
	if e != nil {
		t.Fatal(e)
	}
	before := string(d.Bytes())
	if d.DeleteBackend("b") == nil {
		t.Fatal("conditional mutation allowed")
	}
	if string(d.Bytes()) != before {
		t.Fatal("blocked mutation changed document")
	}
}
func TestZeroWeightAndNoCheckPreserved(t *testing.T) {
	d, e := Parse([]byte("backend b\n server s 127.0.0.1:443 weight 0 no-check send-proxy\n"))
	if e != nil {
		t.Fatal(e)
	}
	srv, e := d.Server("b", "s")
	if e != nil {
		t.Fatal(e)
	}
	if !srv.WeightSet || !srv.CheckDisabled {
		t.Fatal(srv)
	}
	srv.Address = "127.0.0.2"
	if e = d.UpdateServer("b", "s", srv); e != nil {
		t.Fatal(e)
	}
	got := string(d.Bytes())
	for _, want := range []string{"weight 0", "no-check", "send-proxy"} {
		if !strings.Contains(got, want) {
			t.Fatal(got)
		}
	}
}
