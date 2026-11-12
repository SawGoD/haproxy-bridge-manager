package haproxy

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fixture(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "haproxy.cfg"))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestRoundTrip(t *testing.T) {
	inputs := [][]byte{
		nil,
		[]byte("# comment only"),
		[]byte("global\r\n\tdaemon # keep\r\nfrontend f_in\r\n\tbind 127.0.0.1:10443\r\n"),
		[]byte("unknown top-level\n\ndefaults\n\tunknown thing\n"),
		fixture(t),
	}
	for _, input := range inputs {
		document, err := Parse(input)
		if err != nil {
			t.Fatal(err)
		}
		if got := document.Bytes(); !bytes.Equal(got, input) {
			t.Fatalf("round trip changed bytes\nwant: %q\n got: %q", input, got)
		}
	}
}

func TestFixtureModel(t *testing.T) {
	document, _ := Parse(fixture(t))
	frontends, err := document.Frontends()
	if err != nil {
		t.Fatal(err)
	}
	if len(frontends) != 4 {
		t.Fatalf("got %d frontends", len(frontends))
	}
	for i, name := range []string{"de1", "usa1", "lv1", "se1"} {
		if frontends[i].Name != name+"_in" || frontends[i].Bind.Address != "127.0.0.1" || frontends[i].Bind.Port != 10443+i {
			t.Errorf("unexpected frontend: %+v", frontends[i])
		}
		backend, err := document.Backend(name + "_out")
		if err != nil {
			t.Fatal(err)
		}
		if frontends[i].Backend != name+"_out" {
			t.Errorf("unexpected default backend: %+v", frontends[i])
		}
		if len(backend.Servers) != 2 || backend.Servers[0].Role != "" || backend.Servers[1].Role != "" || !backend.Servers[0].HealthCheck {
			t.Errorf("unexpected backend: %+v", backend)
		}
	}
	if err := document.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestTargetedCRUDPreservesUnknownAndLV1(t *testing.T) {
	document, _ := Parse(fixture(t))
	original := document.Bytes()
	if err := document.UpdateFrontend("de1_in", Frontend{Name: "de1_in", Bind: Bind{Address: "127.0.0.1", Port: 11443}}); err != nil {
		t.Fatal(err)
	}
	server, _ := document.Server("de1_out", "de1-netbird")
	server.Address = "100.64.0.110"
	server.HealthCheck = false
	if err := document.UpdateServer("de1_out", "de1-netbird", server); err != nil {
		t.Fatal(err)
	}
	got := string(document.Bytes())
	for _, retained := range []string{"unknown-global-directive preserved", "unknown-backend-directive keep-me", "# lv1 is retained", "maxconn 32", "server de1-netbird 100.64.0.110:110"} {
		if retained == "server de1-netbird 100.64.0.110:110" {
			continue
		}
		if !strings.Contains(got, retained) {
			t.Errorf("missing %q", retained)
		}
	}
	if !strings.Contains(got, "server de1-netbird 100.64.0.110:443 maxconn 32") || strings.Contains(got, "server de1-netbird 100.64.0.110:443 check") {
		t.Fatalf("server update lost options or healthcheck semantics: %s", got)
	}
	if _, err := document.Backend("lv1_out"); err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(original, document.Bytes()) {
		t.Fatal("mutation did not change document")
	}
}

func TestCreateUpdateDelete(t *testing.T) {
	document, _ := Parse([]byte("global\n\tdaemon"))
	if err := document.CreateFrontend(Frontend{Name: "test_in", Bind: Bind{Address: "::1", Port: 10443}}); err != nil {
		t.Fatal(err)
	}
	if err := document.CreateBackend(Backend{Name: "test_out"}); err != nil {
		t.Fatal(err)
	}
	if err := document.UpdateFrontend("test_in", Frontend{Name: "test2_in", Bind: Bind{Address: "::1", Port: 10444}}); err != nil {
		t.Fatal(err)
	}
	if err := document.UpdateBackend("test_out", Backend{Name: "test2_out"}); err != nil {
		t.Fatal(err)
	}
	server := Server{Name: "test-public", Address: "example.com", Port: 443, HealthCheck: true, Role: RolePublic}
	if err := document.CreateServer("test2_out", server); err != nil {
		t.Fatal(err)
	}
	if _, err := document.Server("test2_out", server.Name); err != nil {
		t.Fatal(err)
	}
	if err := document.DeleteServer("test2_out", server.Name); err != nil {
		t.Fatal(err)
	}
	if err := document.DeleteFrontend("test2_in"); err != nil {
		t.Fatal(err)
	}
	if err := document.DeleteBackend("test2_out"); err != nil {
		t.Fatal(err)
	}
	if got := string(document.Bytes()); got != "global\n\tdaemon\n" {
		t.Fatalf("unexpected remaining document: %q", got)
	}
}

func TestReferencedBackendCannotBeDeletedOrRenamed(t *testing.T) {
	document, _ := Parse(fixture(t))
	before := document.Bytes()
	if err := document.DeleteBackend("de1_out"); !errors.Is(err, ErrInUse) {
		t.Fatalf("got %v", err)
	}
	if err := document.UpdateBackend("de1_out", Backend{Name: "renamed"}); !errors.Is(err, ErrInUse) {
		t.Fatalf("got %v", err)
	}
	if !bytes.Equal(before, document.Bytes()) {
		t.Fatal("failed mutation changed document")
	}
	if err := document.DeleteFrontend("de1_in"); err != nil {
		t.Fatal(err)
	}
	if err := document.DeleteBackend("de1_out"); err != nil {
		t.Fatal(err)
	}
	if _, err := document.Backend("lv1_out"); err != nil {
		t.Fatal("lv1 was not retained")
	}
}

func TestAmbiguityRefused(t *testing.T) {
	input := []byte("frontend duplicate\n\tbind 127.0.0.1:1\nfrontend duplicate\n\tbind 127.0.0.1:2\nbackend b\n\tserver s-public 127.0.0.1:443 check\n\tserver s-public 127.0.0.2:443 check\n")
	document, _ := Parse(input)
	before := document.Bytes()
	if err := document.DeleteFrontend("duplicate"); !errors.Is(err, ErrAmbiguous) {
		t.Fatalf("duplicate section: %v", err)
	}
	if err := document.DeleteServer("b", "s-public"); !errors.Is(err, ErrAmbiguous) {
		t.Fatalf("duplicate server: %v", err)
	}
	if !bytes.Equal(before, document.Bytes()) {
		t.Fatal("ambiguous mutation changed bytes")
	}
}

func TestCRLFUsedForNewLines(t *testing.T) {
	document, _ := Parse([]byte("global\r\n\tdaemon"))
	if err := document.CreateBackend(Backend{Name: "new_out"}); err != nil {
		t.Fatal(err)
	}
	if got := string(document.Bytes()); got != "global\r\n\tdaemon\r\nbackend new_out\r\n" {
		t.Fatalf("got %q", got)
	}
}
