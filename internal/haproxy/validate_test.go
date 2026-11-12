package haproxy

import (
	"errors"
	"testing"
)

func TestAddressValidation(t *testing.T) {
	valid := []string{"127.0.0.1", "::1", "2001:db8::1", "localhost", "de1.example.com", "de1-internal"}
	for _, address := range valid {
		if err := ValidateAddress(address); err != nil {
			t.Errorf("%q: %v", address, err)
		}
	}
	invalid := []string{"", "[::1]", "999.1.1.1", "bad_host", "-bad.example", "bad-.example", "bad..example", " 127.0.0.1"}
	for _, address := range invalid {
		if err := ValidateAddress(address); !errors.Is(err, ErrInvalid) {
			t.Errorf("%q: %v", address, err)
		}
	}
}

func TestIdentifierPortAndRoleValidation(t *testing.T) {
	for _, identifier := range []string{"de1_in", "backend-1", "a"} {
		if err := ValidateIdentifier(identifier); err != nil {
			t.Errorf("%q: %v", identifier, err)
		}
	}
	for _, identifier := range []string{"", "1bad", "bad.name", "bad name", "_bad"} {
		if err := ValidateIdentifier(identifier); err == nil {
			t.Errorf("%q accepted", identifier)
		}
	}
	for _, port := range []int{-1, 0, 65536} {
		if err := ValidatePort(port); err == nil {
			t.Errorf("%d accepted", port)
		}
	}
	document, _ := Parse([]byte("backend b\n"))
	server := Server{Name: "s-public", Address: "127.0.0.1", Port: 443, Role: "unknown"}
	if err := document.CreateServer("b", server); !errors.Is(err, ErrInvalid) {
		t.Fatalf("invalid role: %v", err)
	}
}

func TestBindConflictAndAtomicFailure(t *testing.T) {
	document, _ := Parse(fixture(t))
	before := string(document.Bytes())
	frontend := Frontend{Name: "new_in", Bind: Bind{Address: "127.0.0.1", Port: 10445}}
	if err := document.CreateFrontend(frontend); !errors.Is(err, ErrInvalid) {
		t.Fatalf("got %v", err)
	}
	if before != string(document.Bytes()) {
		t.Fatal("conflicting create changed document")
	}
	if err := document.UpdateFrontend("de1_in", frontend); !errors.Is(err, ErrInvalid) {
		t.Fatalf("got %v", err)
	}
}

func TestHealthCheckAndIPv6Endpoints(t *testing.T) {
	document, _ := Parse([]byte("frontend f\n\tbind [::1]:10443\nbackend b\n\tserver s-netbird [2001:db8::1]:443 check\n"))
	frontend, err := document.Frontend("f")
	if err != nil || frontend.Bind.Address != "::1" || frontend.Bind.Port != 10443 {
		t.Fatalf("%+v, %v", frontend, err)
	}
	server, err := document.Server("b", "s-netbird")
	if err != nil || server.Address != "2001:db8::1" || !server.HealthCheck {
		t.Fatalf("%+v, %v", server, err)
	}
}

func TestCandidateValidator(t *testing.T) {
	if err := (Validator{}).Validate(fixture(t)); err != nil {
		t.Fatal(err)
	}
	if err := (Validator{}).Validate([]byte("frontend bad\n\tbind bad endpoint\n")); err == nil {
		t.Fatal("invalid candidate accepted")
	}
}
