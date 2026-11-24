package haproxy

import (
	"fmt"
	"net"
	"regexp"
	"strconv"
	"strings"
)

var (
	identifierPattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]{0,63}$`)
	durationPattern   = regexp.MustCompile(`^[0-9]+(?:us|ms|s|m|h|d)?$`)
)

func ValidateIdentifier(value string) error {
	if !identifierPattern.MatchString(value) {
		return fmt.Errorf("%w: identifier %q", ErrInvalid, value)
	}
	return nil
}

func ValidateAddress(value string) error {
	if value == "" || strings.TrimSpace(value) != value {
		return fmt.Errorf("%w: empty or padded address", ErrInvalid)
	}
	if ip := net.ParseIP(value); ip != nil {
		return nil
	}
	if net.ParseIP(strings.Trim(value, "[]")) != nil {
		return fmt.Errorf("%w: IPv6 address %q must not use brackets", ErrInvalid, value)
	}
	if len(value) > 253 || strings.HasPrefix(value, ".") || strings.HasSuffix(value, ".") {
		return fmt.Errorf("%w: hostname %q", ErrInvalid, value)
	}
	numericIPv4 := true
	for _, r := range value {
		if (r < '0' || r > '9') && r != '.' {
			numericIPv4 = false
			break
		}
	}
	if numericIPv4 {
		return fmt.Errorf("%w: IPv4 address %q", ErrInvalid, value)
	}
	for _, label := range strings.Split(value, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return fmt.Errorf("%w: hostname %q", ErrInvalid, value)
		}
		for _, r := range label {
			if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') && r != '-' {
				return fmt.Errorf("%w: hostname %q", ErrInvalid, value)
			}
		}
	}
	return nil
}

func ValidatePort(port int) error {
	if port < 1 || port > 65535 {
		return fmt.Errorf("%w: port %d", ErrInvalid, port)
	}
	return nil
}

func validateDuration(value, directive string) error {
	if !durationPattern.MatchString(value) {
		return fmt.Errorf("%w: %s duration %q", ErrInvalid, directive, value)
	}
	return nil
}

func validateFrontend(frontend Frontend) error {
	if err := ValidateIdentifier(frontend.Name); err != nil {
		return err
	}
	if frontend.Backend != "" {
		if err := ValidateIdentifier(frontend.Backend); err != nil {
			return err
		}
	}
	if err := ValidateAddress(frontend.Bind.Address); err != nil {
		return err
	}
	if err := ValidatePort(frontend.Bind.Port); err != nil {
		return err
	}
	for directive, value := range map[string]string{"client": frontend.TimeoutClient, "server": frontend.TimeoutServer} {
		if value != "" {
			if err := validateDuration(value, "timeout "+directive); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateBackend(backend Backend) error {
	if err := ValidateIdentifier(backend.Name); err != nil {
		return err
	}
	for directive, value := range map[string]string{"connect": backend.TimeoutConnect, "client": backend.TimeoutClient, "server": backend.TimeoutServer} {
		if value != "" {
			if err := validateDuration(value, "timeout "+directive); err != nil {
				return err
			}
		}
	}
	seen := make(map[string]struct{})
	for _, server := range backend.Servers {
		if err := ValidateServer(server); err != nil {
			return err
		}
		if _, ok := seen[server.Name]; ok {
			return fmt.Errorf("%w: duplicate server %q", ErrAmbiguous, server.Name)
		}
		seen[server.Name] = struct{}{}
	}
	return nil
}

func ValidateServer(server Server) error {
	if err := ValidateIdentifier(server.Name); err != nil {
		return err
	}
	if err := ValidateAddress(server.Address); err != nil {
		return err
	}
	if err := ValidatePort(server.Port); err != nil {
		return err
	}
	if server.Role != "" && server.Role != RolePublic && server.Role != RoleNetbird {
		return fmt.Errorf("%w: role %q", ErrInvalid, server.Role)
	}
	if server.Weight < 0 || server.Weight > 256 {
		return fmt.Errorf("%w: weight %d", ErrInvalid, server.Weight)
	}
	if server.Fall < 0 || server.Rise < 0 {
		return fmt.Errorf("%w: negative health check threshold", ErrInvalid)
	}
	if server.Interval != "" {
		if err := validateDuration(server.Interval, "inter"); err != nil {
			return err
		}
	}
	return nil
}

func splitEndpoint(endpoint string) (string, int, error) {
	host, portText, err := net.SplitHostPort(endpoint)
	if err != nil {
		return "", 0, fmt.Errorf("%w: endpoint %q", ErrInvalid, endpoint)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		return "", 0, fmt.Errorf("%w: endpoint %q", ErrInvalid, endpoint)
	}
	return host, port, nil
}

func formatEndpoint(address string, port int) string {
	if net.ParseIP(address) != nil && strings.Contains(address, ":") {
		return "[" + address + "]:" + strconv.Itoa(port)
	}
	return net.JoinHostPort(address, strconv.Itoa(port))
}

func endpointKey(address string, port int) string {
	if ip := net.ParseIP(address); ip != nil {
		address = ip.String()
	} else {
		address = strings.ToLower(address)
	}
	return formatEndpoint(address, port)
}

type proxyBind struct {
	kind    SectionKind
	name    string
	address string
	port    int
}

func (d *Document) proxyBinds() ([]proxyBind, error) {
	var result []proxyBind
	for _, s := range d.sections {
		if s.kind != SectionFrontend && s.kind != SectionListen {
			continue
		}
		for i := s.start + 1; i < s.end; i++ {
			fields := headerFields(d.lines[i].raw)
			if len(fields) == 0 || fields[0] != "bind" {
				continue
			}
			if len(fields) < 2 {
				return nil, fmt.Errorf("%w: %s %q has an incomplete bind", ErrInvalid, s.kind, s.name)
			}
			address, port, err := splitEndpoint(fields[1])
			if err != nil {
				return nil, fmt.Errorf("%w: %s %q: %v", ErrInvalid, s.kind, s.name, err)
			}
			result = append(result, proxyBind{kind: s.kind, name: s.name, address: address, port: port})
		}
	}
	return result, nil
}

func bindsConflict(leftAddress string, leftPort int, rightAddress string, rightPort int) bool {
	if leftPort != rightPort {
		return false
	}
	if leftAddress == "0.0.0.0" || leftAddress == "::" || rightAddress == "0.0.0.0" || rightAddress == "::" {
		return true
	}
	return endpointKey(leftAddress, leftPort) == endpointKey(rightAddress, rightPort)
}

func (d *Document) checkBindConflict(bind Bind, except string) error {
	binds, err := d.proxyBinds()
	if err != nil {
		return err
	}
	for _, existing := range binds {
		if existing.kind == SectionFrontend && existing.name == except {
			continue
		}
		if bindsConflict(bind.Address, bind.Port, existing.address, existing.port) {
			return fmt.Errorf("%w: bind conflicts with %s %q", ErrInvalid, existing.kind, existing.name)
		}
	}
	return nil
}

// NextFreePort returns the first port at or above start absent from all TCP binds.
// Zero means the range is exhausted.
func (d *Document) NextFreePort(start int) (int, error) {
	binds, err := d.proxyBinds()
	if err != nil {
		return 0, err
	}
	used := make(map[int]bool, len(binds))
	for _, bind := range binds {
		used[bind.port] = true
	}
	for port := max(1, start); port <= 65535; port++ {
		if !used[port] {
			return port, nil
		}
	}
	return 0, nil
}

func (d *Document) Validate() error {
	sectionNames := make(map[string]struct{})
	proxyNames := make(map[string]SectionKind)
	for _, s := range d.sections {
		if s.name != "" {
			key := string(s.kind) + "\x00" + s.name
			if _, ok := sectionNames[key]; ok {
				return fmt.Errorf("%w: duplicate %s %q", ErrAmbiguous, s.kind, s.name)
			}
			sectionNames[key] = struct{}{}
		}
		switch s.kind {
		case SectionFrontend, SectionBackend, SectionListen:
			if err := ValidateIdentifier(s.name); err != nil {
				return err
			}
			if previous, ok := proxyNames[s.name]; ok {
				return fmt.Errorf("%w: duplicate proxy identifier %q (%s and %s)", ErrAmbiguous, s.name, previous, s.kind)
			}
			proxyNames[s.name] = s.kind
		case SectionDefaults:
			if s.name != "" {
				if err := ValidateIdentifier(s.name); err != nil {
					return err
				}
			}
		}
	}
	frontends, err := d.Frontends()
	if err != nil {
		return err
	}
	for _, frontend := range frontends {
		if err := validateFrontend(frontend); err != nil {
			return err
		}
	}
	backends, err := d.Backends()
	if err != nil {
		return err
	}
	backendNames := make(map[string]struct{})
	for _, backend := range backends {
		backendNames[backend.Name] = struct{}{}
		if err := validateBackend(backend); err != nil {
			return err
		}
	}
	if _, err := d.DefaultsSections(); err != nil {
		return err
	}
	binds, err := d.proxyBinds()
	if err != nil {
		return err
	}
	for _, bind := range binds {
		if err := ValidateAddress(bind.address); err != nil {
			return err
		}
		if err := ValidatePort(bind.port); err != nil {
			return err
		}
	}
	for i := range binds {
		for j := i + 1; j < len(binds); j++ {
			if bindsConflict(binds[i].address, binds[i].port, binds[j].address, binds[j].port) {
				return fmt.Errorf("%w: bind conflict between %s %q and %s %q", ErrInvalid, binds[i].kind, binds[i].name, binds[j].kind, binds[j].name)
			}
		}
	}
	for _, s := range d.sections {
		if s.kind != SectionFrontend && s.kind != SectionListen {
			continue
		}
		for i := s.start + 1; i < s.end; i++ {
			fields := headerFields(d.lines[i].raw)
			if len(fields) == 0 || (fields[0] != "default_backend" && fields[0] != "use_backend") {
				continue
			}
			if len(fields) < 2 {
				return fmt.Errorf("%w: %s %q has incomplete %s", ErrInvalid, s.kind, s.name, fields[0])
			}
			if _, ok := backendNames[fields[1]]; !ok {
				return fmt.Errorf("%w: %s %q references missing backend %q", ErrNotFound, s.kind, s.name, fields[1])
			}
		}
	}
	return nil
}

type Validator struct{}

func (Validator) Validate(data []byte) error {
	document, err := Parse(data)
	if err != nil {
		return err
	}
	return document.Validate()
}
