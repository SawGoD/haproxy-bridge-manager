package haproxy

import (
	"fmt"
	"strconv"
)

type Bind struct {
	Address string
	Port    int
}

type Frontend struct {
	Name          string
	Backend       string
	Bind          Bind
	TimeoutClient string
	TimeoutServer string
}

type Role string

const (
	RolePublic  Role = "public"
	RoleNetbird Role = "netbird"
)

type Server struct {
	Name          string
	Address       string
	Port          int
	HealthCheck   bool
	CheckDisabled bool
	WeightSet     bool
	Role          Role
	Backup        bool
	Disabled      bool
	Weight        int
	Interval      string
	Fall          int
	Rise          int
}

type Backend struct {
	Name           string
	Servers        []Server
	TimeoutConnect string
	TimeoutClient  string
	TimeoutServer  string
}

type Defaults struct {
	Name           string
	TimeoutConnect string
	TimeoutClient  string
	TimeoutServer  string
}

func (d *Document) Frontends() ([]Frontend, error) {
	var result []Frontend
	for i := range d.sections {
		if d.sections[i].kind != SectionFrontend {
			continue
		}
		frontend, err := d.frontendAt(i)
		if err != nil {
			return nil, err
		}
		result = append(result, frontend)
	}
	return result, nil
}

func (d *Document) Frontend(name string) (Frontend, error) {
	index, err := d.uniqueSection(SectionFrontend, name)
	if err != nil {
		return Frontend{}, err
	}
	return d.frontendAt(index)
}

func (d *Document) frontendAt(index int) (Frontend, error) {
	s := d.sections[index]
	result := Frontend{Name: s.name}
	bindLines := 0
	backendLines := 0
	for i := s.start + 1; i < s.end; i++ {
		fields := headerFields(d.lines[i].raw)
		if len(fields) == 0 {
			continue
		}
		switch fields[0] {
		case "bind":
			bindLines++
			if len(fields) < 2 {
				return Frontend{}, fmt.Errorf("%w: frontend %q has an incomplete bind", ErrAmbiguous, s.name)
			}
			address, port, err := splitEndpoint(fields[1])
			if err != nil {
				return Frontend{}, fmt.Errorf("%w: frontend %q: %v", ErrAmbiguous, s.name, err)
			}
			result.Bind = Bind{Address: address, Port: port}
		case "default_backend":
			backendLines++
			if len(fields) != 2 {
				return Frontend{}, fmt.Errorf("%w: frontend %q has a malformed default_backend", ErrInvalid, s.name)
			}
			result.Backend = fields[1]
		case "timeout":
			if len(fields) != 3 {
				return Frontend{}, fmt.Errorf("%w: frontend %q has a malformed timeout", ErrInvalid, s.name)
			}
			switch fields[1] {
			case "client":
				if result.TimeoutClient != "" {
					return Frontend{}, fmt.Errorf("%w: frontend %q has duplicate timeout client", ErrAmbiguous, s.name)
				}
				result.TimeoutClient = fields[2]
			case "server":
				if result.TimeoutServer != "" {
					return Frontend{}, fmt.Errorf("%w: frontend %q has duplicate timeout server", ErrAmbiguous, s.name)
				}
				result.TimeoutServer = fields[2]
			}
		}
	}
	if bindLines > 1 {
		return Frontend{}, fmt.Errorf("%w: frontend %q has multiple binds", ErrAmbiguous, s.name)
	}
	if backendLines > 1 {
		return Frontend{}, fmt.Errorf("%w: frontend %q has multiple default_backend directives", ErrAmbiguous, s.name)
	}
	return result, nil
}

func (d *Document) Backends() ([]Backend, error) {
	var result []Backend
	for i := range d.sections {
		if d.sections[i].kind != SectionBackend {
			continue
		}
		backend, err := d.backendAt(i)
		if err != nil {
			return nil, err
		}
		result = append(result, backend)
	}
	return result, nil
}

func (d *Document) Backend(name string) (Backend, error) {
	index, err := d.uniqueSection(SectionBackend, name)
	if err != nil {
		return Backend{}, err
	}
	return d.backendAt(index)
}

func (d *Document) backendAt(index int) (Backend, error) {
	s := d.sections[index]
	result := Backend{Name: s.name}
	for i := s.start + 1; i < s.end; i++ {
		fields := headerFields(d.lines[i].raw)
		if len(fields) == 0 {
			continue
		}
		if fields[0] == "timeout" {
			if len(fields) != 3 {
				return Backend{}, fmt.Errorf("%w: backend %q has a malformed timeout", ErrInvalid, s.name)
			}
			var target *string
			switch fields[1] {
			case "connect":
				target = &result.TimeoutConnect
			case "client":
				target = &result.TimeoutClient
			case "server":
				target = &result.TimeoutServer
			}
			if target != nil {
				if *target != "" {
					return Backend{}, fmt.Errorf("%w: backend %q has duplicate timeout %s", ErrAmbiguous, s.name, fields[1])
				}
				*target = fields[2]
			}
			continue
		}
		if fields[0] != "server" {
			continue
		}
		if len(fields) < 3 {
			return Backend{}, fmt.Errorf("%w: backend %q has an incomplete server", ErrAmbiguous, s.name)
		}
		address, port, err := splitEndpoint(fields[2])
		if err != nil {
			return Backend{}, fmt.Errorf("%w: backend %q: %v", ErrAmbiguous, s.name, err)
		}
		server := Server{Name: fields[1], Address: address, Port: port}
		for option := 3; option < len(fields); option++ {
			switch fields[option] {
			case "check":
				server.HealthCheck = true
			case "no-check":
				server.CheckDisabled = true
			case "backup":
				server.Backup = true
			case "disabled":
				server.Disabled = true
			case "weight":
				value, next, err := serverOptionValue(fields, option, s.name, server.Name)
				if err != nil {
					return Backend{}, err
				}
				server.WeightSet = true
				server.Weight, err = strconv.Atoi(value)
				if err != nil {
					return Backend{}, fmt.Errorf("%w: backend %q server %q has invalid weight", ErrInvalid, s.name, server.Name)
				}
				option = next
			case "inter":
				value, next, err := serverOptionValue(fields, option, s.name, server.Name)
				if err != nil {
					return Backend{}, err
				}
				server.Interval = value
				option = next
			case "fall":
				value, next, err := serverOptionValue(fields, option, s.name, server.Name)
				if err != nil {
					return Backend{}, err
				}
				server.Fall, err = strconv.Atoi(value)
				if err != nil {
					return Backend{}, fmt.Errorf("%w: backend %q server %q has invalid fall", ErrInvalid, s.name, server.Name)
				}
				option = next
			case "rise":
				value, next, err := serverOptionValue(fields, option, s.name, server.Name)
				if err != nil {
					return Backend{}, err
				}
				server.Rise, err = strconv.Atoi(value)
				if err != nil {
					return Backend{}, fmt.Errorf("%w: backend %q server %q has invalid rise", ErrInvalid, s.name, server.Name)
				}
				option = next
			}
		}
		result.Servers = append(result.Servers, server)
	}
	return result, nil
}

func serverOptionValue(fields []string, option int, backend, server string) (string, int, error) {
	if option+1 >= len(fields) {
		return "", option, fmt.Errorf("%w: backend %q server %q has incomplete %s option", ErrInvalid, backend, server, fields[option])
	}
	return fields[option+1], option + 1, nil
}

func (d *Document) DefaultsSections() ([]Defaults, error) {
	var result []Defaults
	for _, s := range d.sections {
		if s.kind != SectionDefaults {
			continue
		}
		entry := Defaults{Name: s.name}
		for line := s.start + 1; line < s.end; line++ {
			fields := headerFields(d.lines[line].raw)
			if len(fields) == 0 || fields[0] != "timeout" {
				continue
			}
			if len(fields) != 3 {
				return nil, fmt.Errorf("%w: defaults %q has a malformed timeout", ErrInvalid, s.name)
			}
			var target *string
			switch fields[1] {
			case "connect":
				target = &entry.TimeoutConnect
			case "client":
				target = &entry.TimeoutClient
			case "server":
				target = &entry.TimeoutServer
			}
			if target != nil {
				if *target != "" {
					return nil, fmt.Errorf("%w: defaults %q has duplicate timeout %s", ErrAmbiguous, s.name, fields[1])
				}
				*target = fields[2]
			}
		}
		result = append(result, entry)
	}
	return result, nil
}

func (d *Document) Server(backendName, serverName string) (Server, error) {
	backend, err := d.Backend(backendName)
	if err != nil {
		return Server{}, err
	}
	found := -1
	for i, server := range backend.Servers {
		if server.Name == serverName {
			if found >= 0 {
				return Server{}, fmt.Errorf("%w: server %q", ErrAmbiguous, serverName)
			}
			found = i
		}
	}
	if found < 0 {
		return Server{}, ErrNotFound
	}
	return backend.Servers[found], nil
}

func (d *Document) uniqueSection(kind SectionKind, name string) (int, error) {
	found := -1
	for i, s := range d.sections {
		if s.kind == kind && s.name == name {
			if found >= 0 {
				return -1, fmt.Errorf("%w: %s %q", ErrAmbiguous, kind, name)
			}
			found = i
		}
	}
	if found < 0 {
		return -1, ErrNotFound
	}
	return found, nil
}
