package haproxy

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestUnknownSectionsTerminateBackendAndNamedDefaults(t *testing.T) {
	input := []byte("defaults named\n\ttimeout connect 2s\nbackend obsolete\n\tserver old 127.0.0.1:443\nresolvers dns\n\tserver ns 127.0.0.1:53\npeers replication\n\tpeer local 127.0.0.1:10000\nuserlist users\n\tuser user password password\ncustom-section custom\n\tarbitrary value\nbackend retained\n")
	document, err := Parse(input)
	if err != nil {
		t.Fatal(err)
	}
	defaults, err := document.DefaultsSections()
	if err != nil || len(defaults) != 1 || defaults[0].Name != "named" || defaults[0].TimeoutConnect != "2s" {
		t.Fatalf("named defaults not modeled: %+v %v", defaults, err)
	}
	if err := document.DeleteBackend("obsolete"); err != nil {
		t.Fatal(err)
	}
	got := string(document.Bytes())
	for _, retained := range []string{"resolvers dns", "peers replication", "userlist users", "custom-section custom", "backend retained"} {
		if !strings.Contains(got, retained) {
			t.Errorf("deleted unrelated section containing %q: %s", retained, got)
		}
	}
}

func TestMalformedSectionHeadersReturnErrors(t *testing.T) {
	for _, input := range []string{"frontend\n", "backend name extra\n", "listen\n", "defaults one two\n", "global extra\n", "resolvers\n", "peers\n", "userlist\n"} {
		if _, err := Parse([]byte(input)); !errors.Is(err, ErrInvalid) {
			t.Errorf("%q: got %v", input, err)
		}
	}
}

func TestFrontendDefaultBackendCRUDAndMissingReferences(t *testing.T) {
	document, err := Parse([]byte("backend selected\n"))
	if err != nil {
		t.Fatal(err)
	}
	frontend := Frontend{Name: "f", Backend: "selected", Bind: Bind{Address: "127.0.0.1", Port: 8443}}
	if err := document.CreateFrontend(frontend); err != nil {
		t.Fatal(err)
	}
	parsed, err := document.Frontend("f")
	if err != nil || parsed.Backend != "selected" || !strings.Contains(string(document.Bytes()), "default_backend selected") {
		t.Fatalf("default backend not rendered: %+v %v\n%s", parsed, err, document.Bytes())
	}
	frontend.TimeoutClient = "30s"
	frontend.TimeoutServer = "31s"
	if err := document.UpdateFrontend("f", frontend); err != nil {
		t.Fatal(err)
	}
	parsed, _ = document.Frontend("f")
	if parsed.TimeoutClient != "30s" || parsed.TimeoutServer != "31s" {
		t.Fatalf("timeouts not rendered: %+v", parsed)
	}
	missing := Frontend{Name: "missing", Backend: "absent", Bind: Bind{Address: "127.0.0.1", Port: 8444}}
	if err := document.CreateFrontend(missing); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing create reference accepted: %v", err)
	}
	invalid, _ := Parse([]byte("frontend f\n\tbind 127.0.0.1:1\n\tdefault_backend absent\n"))
	if err := invalid.Validate(); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing validation reference accepted: %v", err)
	}
}

func TestDuplicateIdentifiersAcrossSections(t *testing.T) {
	for _, input := range []string{
		"defaults duplicate\ndefaults duplicate\n",
		"resolvers duplicate\nresolvers duplicate\n",
		"frontend same\n\tbind 127.0.0.1:1\nlisten same\n\tbind 127.0.0.1:2\n",
	} {
		document, err := Parse([]byte(input))
		if err != nil {
			t.Fatal(err)
		}
		if err := document.Validate(); !errors.Is(err, ErrAmbiguous) {
			t.Errorf("%q: got %v", input, err)
		}
	}
}

func TestServerOptionsRoundTripAndUnknownPreservation(t *testing.T) {
	input := []byte("backend b\n\tserver imported 127.0.0.1:443 check backup disabled weight 17 inter 2s fall 3 rise 4 maxconn 9 slowstart 30s # keep comment\n")
	document, err := Parse(input)
	if err != nil {
		t.Fatal(err)
	}
	server, err := document.Server("b", "imported")
	if err != nil {
		t.Fatal(err)
	}
	if server.Role != "" || !server.HealthCheck || !server.Backup || !server.Disabled || server.Weight != 17 || server.Interval != "2s" || server.Fall != 3 || server.Rise != 4 {
		t.Fatalf("options not parsed: %+v", server)
	}
	server.HealthCheck = false
	server.Disabled = false
	server.Weight = 20
	server.Interval = "5s"
	server.Fall = 6
	server.Rise = 7
	if err := document.UpdateServer("b", "imported", server); err != nil {
		t.Fatal(err)
	}
	got := string(document.Bytes())
	for _, retained := range []string{"maxconn 9", "slowstart 30s", "# keep comment", "backup", "weight 20", "inter 5s", "fall 6", "rise 7"} {
		if !strings.Contains(got, retained) {
			t.Errorf("missing %q: %s", retained, got)
		}
	}
	if strings.Contains(got, "check ") || strings.Contains(got, "disabled") || strings.Contains(got, "weight 17") {
		t.Fatalf("stale managed options remain: %s", got)
	}
	if err := document.CreateServer("b", Server{Name: "emptyrole", Address: "127.0.0.2", Port: 443}); err != nil {
		t.Fatalf("empty imported role rejected: %v", err)
	}
	if err := document.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestBackendTimeoutsRenderWithoutReplacingServers(t *testing.T) {
	document, _ := Parse(nil)
	backend := Backend{Name: "b", TimeoutConnect: "5s", TimeoutClient: "1m", TimeoutServer: "2m", Servers: []Server{{Name: "s", Address: "127.0.0.1", Port: 443, Backup: true, HealthCheck: true}}}
	if err := document.CreateBackend(backend); err != nil {
		t.Fatal(err)
	}
	original := document.Bytes()
	backend.TimeoutConnect = "6s"
	if err := document.UpdateBackend("b", backend); err != nil {
		t.Fatal(err)
	}
	got, err := document.Backend("b")
	if err != nil || got.TimeoutConnect != "6s" || len(got.Servers) != 1 || !got.Servers[0].Backup {
		t.Fatalf("backend update failed: %+v %v", got, err)
	}
	if bytes.Contains(original, []byte("timeout connect 6s")) || !strings.Contains(string(document.Bytes()), "timeout connect 6s") {
		t.Fatal("timeout was not updated")
	}
}

func TestWildcardBindConflictsIncludeListen(t *testing.T) {
	document, _ := Parse([]byte("listen existing\n\tbind 0.0.0.0:443\n"))
	frontend := Frontend{Name: "f", Bind: Bind{Address: "127.0.0.1", Port: 443}}
	if err := document.CreateFrontend(frontend); !errors.Is(err, ErrInvalid) {
		t.Fatalf("wildcard/listen conflict accepted: %v", err)
	}
	invalid, _ := Parse([]byte("listen one\n\tbind [::]:443\nlisten two\n\tbind 127.0.0.1:443\n"))
	if err := invalid.Validate(); !errors.Is(err, ErrInvalid) {
		t.Fatalf("IPv6 wildcard conflict accepted: %v", err)
	}
}
