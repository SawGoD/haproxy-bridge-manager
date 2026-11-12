package network

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestTCP(t *testing.T) {
	l, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	defer l.Close()
	go func() {
		c, e := l.Accept()
		if e == nil {
			c.Close()
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	r := Probe(ctx, l.Addr().String(), "", false)
	if !r.TCP || r.Error != "" {
		t.Fatal(r)
	}
}
func TestTLSUntrusted(t *testing.T) {
	s := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer s.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	r := Probe(ctx, s.Listener.Addr().String(), "example.com", true)
	if !r.TCP || r.CertificateValid || r.Error == "" || r.TLSVersion == "" {
		t.Fatal(r)
	}
}
func TestCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if Probe(ctx, "127.0.0.1:1", "", false).TCP {
		t.Fatal("cancel ignored")
	}
}
