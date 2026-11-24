package haproxy

import (
	"bytes"
	"fmt"
	"slices"
	"strconv"
	"strings"
)

// CopyRoute duplicates the source proxy sections beside their originals so that
// they remain under the same defaults, then updates the new frontend's bind.
func (d *Document) CopyRoute(source string, target Frontend) error {
	if err := d.ensureEditable(); err != nil {
		return err
	}
	if err := validateFrontend(target); err != nil {
		return err
	}
	if target.Backend == "" || target.Backend == target.Name {
		return fmt.Errorf("%w: copy requires a distinct backend name", ErrInvalid)
	}
	for _, name := range []string{target.Name, target.Backend} {
		if err := d.nameAvailable(name, -1); err != nil {
			return fmt.Errorf("%w: proxy %q", err, name)
		}
	}
	frontend, err := d.Frontend(source)
	if err != nil {
		return err
	}
	if err := d.checkBindConflict(target.Bind, ""); err != nil {
		return err
	}
	work := d.clone()
	if err := work.copyProxy(SectionBackend, frontend.Backend, target.Backend, "", ""); err != nil {
		return err
	}
	if err := work.copyProxy(SectionFrontend, source, target.Name, frontend.Backend, target.Backend); err != nil {
		return err
	}
	if err := work.UpdateFrontend(target.Name, target); err != nil {
		return err
	}
	*d = *work
	return nil
}

func (d *Document) copyProxy(kind SectionKind, source, name, oldBackend, newBackend string) error {
	i, err := d.uniqueSection(kind, source)
	if err != nil {
		return err
	}
	s := d.sections[i]
	duplicate := []line{{raw: replaceSectionName(d.lines[s.start].raw, name, d.newline)}}
	for _, l := range d.lines[s.start+1 : s.end] {
		fields := headerFields(l.raw)
		if len(fields) > 0 && fields[0] == "id" {
			continue // HAProxy assigns a new proxy ID.
		}
		raw := l.raw
		if oldBackend != "" && len(fields) >= 2 && fields[0] == "use_backend" && fields[1] == oldBackend {
			tokens := fieldsAt(raw)
			tokens[1] = newBackend
			raw = []byte(string(indent(raw)) + strings.Join(tokens, " ") + string(lineEnding(raw, d.newline)))
		}
		duplicate = append(duplicate, line{raw: raw})
	}
	last := &d.lines[s.end-1].raw
	if !bytes.HasSuffix(*last, []byte("\n")) {
		*last = append(*last, d.newline...)
	}
	d.lines = slices.Insert(d.lines, s.end, duplicate...)
	d.reparse()
	return nil
}

func (d *Document) CreateFrontend(frontend Frontend) error {
	if err := d.ensureEditable(); err != nil {
		return err
	}
	if err := validateFrontend(frontend); err != nil {
		return err
	}
	if err := d.nameAvailable(frontend.Name, -1); err != nil {
		return err
	}
	if err := d.requireBackend(frontend.Backend); err != nil {
		return err
	}
	if err := d.checkBindConflict(frontend.Bind, ""); err != nil {
		return err
	}
	lines := []string{
		"frontend " + frontend.Name,
		"\tbind " + formatEndpoint(frontend.Bind.Address, frontend.Bind.Port),
	}
	if frontend.Backend != "" {
		lines = append(lines, "\tdefault_backend "+frontend.Backend)
	}
	lines = appendTimeout(lines, "client", frontend.TimeoutClient)
	lines = appendTimeout(lines, "server", frontend.TimeoutServer)
	work := d.clone()
	work.appendSection(lines)
	*d = *work
	return nil
}

func (d *Document) UpdateFrontend(name string, frontend Frontend) error {
	if err := d.ensureEditable(); err != nil {
		return err
	}
	if err := validateFrontend(frontend); err != nil {
		return err
	}
	if err := d.checkBindConflict(frontend.Bind, name); err != nil {
		return err
	}
	if err := d.requireBackend(frontend.Backend); err != nil {
		return err
	}
	work := d.clone()
	index, err := work.uniqueSection(SectionFrontend, name)
	if err != nil {
		return err
	}
	if err := work.nameAvailable(frontend.Name, index); err != nil {
		return err
	}
	s := work.sections[index]
	work.lines[s.start].raw = replaceSectionName(work.lines[s.start].raw, frontend.Name, work.newline)
	if err := work.setDirective(&s, []string{"bind"}, formatEndpoint(frontend.Bind.Address, frontend.Bind.Port)); err != nil {
		return err
	}
	if err := work.setDirective(&s, []string{"default_backend"}, frontend.Backend); err != nil {
		return err
	}
	if err := work.setDirective(&s, []string{"timeout", "client"}, frontend.TimeoutClient); err != nil {
		return err
	}
	if err := work.setDirective(&s, []string{"timeout", "server"}, frontend.TimeoutServer); err != nil {
		return err
	}
	work.reparse()
	*d = *work
	return nil
}

func (d *Document) DeleteFrontend(name string) error {
	if err := d.ensureEditable(); err != nil {
		return err
	}
	work := d.clone()
	index, err := work.uniqueSection(SectionFrontend, name)
	if err != nil {
		return err
	}
	work.removeSection(index)
	*d = *work
	return nil
}

func (d *Document) CreateBackend(backend Backend) error {
	if err := d.ensureEditable(); err != nil {
		return err
	}
	if err := validateBackend(backend); err != nil {
		return err
	}
	if err := d.nameAvailable(backend.Name, -1); err != nil {
		return err
	}
	lines := []string{"backend " + backend.Name}
	lines = appendTimeout(lines, "connect", backend.TimeoutConnect)
	lines = appendTimeout(lines, "client", backend.TimeoutClient)
	lines = appendTimeout(lines, "server", backend.TimeoutServer)
	for _, server := range backend.Servers {
		lines = append(lines, "\t"+renderServer(server, nil))
	}
	work := d.clone()
	work.appendSection(lines)
	*d = *work
	return nil
}

func (d *Document) UpdateBackend(name string, backend Backend) error {
	if err := d.ensureEditable(); err != nil {
		return err
	}
	if err := validateBackend(backend); err != nil {
		return err
	}
	work := d.clone()
	index, err := work.uniqueSection(SectionBackend, name)
	if err != nil {
		return err
	}
	if err := work.nameAvailable(backend.Name, index); err != nil {
		return err
	}
	if backend.Name != name && len(work.backendReferences(name)) > 0 {
		return fmt.Errorf("%w: cannot rename %q", ErrInUse, name)
	}
	s := work.sections[index]
	work.lines[s.start].raw = replaceSectionName(work.lines[s.start].raw, backend.Name, work.newline)
	if err := work.setDirective(&s, []string{"timeout", "connect"}, backend.TimeoutConnect); err != nil {
		return err
	}
	if err := work.setDirective(&s, []string{"timeout", "client"}, backend.TimeoutClient); err != nil {
		return err
	}
	if err := work.setDirective(&s, []string{"timeout", "server"}, backend.TimeoutServer); err != nil {
		return err
	}
	work.reparse()
	*d = *work
	return nil
}

func (d *Document) DeleteBackend(name string) error {
	if err := d.ensureEditable(); err != nil {
		return err
	}
	work := d.clone()
	index, err := work.uniqueSection(SectionBackend, name)
	if err != nil {
		return err
	}
	if refs := work.backendReferences(name); len(refs) > 0 {
		return fmt.Errorf("%w: %q referenced by %s", ErrInUse, name, strings.Join(refs, ", "))
	}
	work.removeSection(index)
	*d = *work
	return nil
}

func (d *Document) CreateServer(backendName string, server Server) error {
	if err := d.ensureEditable(); err != nil {
		return err
	}
	if err := ValidateServer(server); err != nil {
		return err
	}
	work := d.clone()
	index, err := work.uniqueSection(SectionBackend, backendName)
	if err != nil {
		return err
	}
	s := work.sections[index]
	for i := s.start + 1; i < s.end; i++ {
		fields := headerFields(work.lines[i].raw)
		if len(fields) > 1 && fields[0] == "server" && fields[1] == server.Name {
			return ErrExists
		}
	}
	raw := []byte("\t" + renderServer(server, nil) + string(work.newline))
	work.insertLine(s.end, raw)
	*d = *work
	return nil
}

func (d *Document) UpdateServer(backendName, serverName string, server Server) error {
	if err := d.ensureEditable(); err != nil {
		return err
	}
	if err := ValidateServer(server); err != nil {
		return err
	}
	work := d.clone()
	index, err := work.uniqueSection(SectionBackend, backendName)
	if err != nil {
		return err
	}
	s := work.sections[index]
	found := -1
	for i := s.start + 1; i < s.end; i++ {
		fields := headerFields(work.lines[i].raw)
		if len(fields) > 1 && fields[0] == "server" && fields[1] == serverName {
			if found >= 0 {
				return fmt.Errorf("%w: server %q", ErrAmbiguous, serverName)
			}
			found = i
		}
		if len(fields) > 1 && fields[0] == "server" && fields[1] == server.Name && serverName != server.Name {
			return ErrExists
		}
	}
	if found < 0 {
		return ErrNotFound
	}
	work.lines[found].raw = []byte(renderServer(server, work.lines[found].raw))
	*d = *work
	return nil
}

func (d *Document) DeleteServer(backendName, serverName string) error {
	if err := d.ensureEditable(); err != nil {
		return err
	}
	work := d.clone()
	index, err := work.uniqueSection(SectionBackend, backendName)
	if err != nil {
		return err
	}
	s := work.sections[index]
	found := -1
	for i := s.start + 1; i < s.end; i++ {
		fields := headerFields(work.lines[i].raw)
		if len(fields) > 1 && fields[0] == "server" && fields[1] == serverName {
			if found >= 0 {
				return fmt.Errorf("%w: server %q", ErrAmbiguous, serverName)
			}
			found = i
		}
	}
	if found < 0 {
		return ErrNotFound
	}
	work.lines = append(work.lines[:found], work.lines[found+1:]...)
	work.reparse()
	*d = *work
	return nil
}

func (d *Document) requireBackend(name string) error {
	if name == "" {
		return nil
	}
	if _, err := d.uniqueSection(SectionBackend, name); err != nil {
		return fmt.Errorf("%w: default backend %q", ErrNotFound, name)
	}
	return nil
}

func (d *Document) nameAvailable(name string, except int) error {
	for i, s := range d.sections {
		if i != except && s.name == name && (s.kind == SectionFrontend || s.kind == SectionBackend || s.kind == SectionListen) {
			return ErrExists
		}
	}
	return nil
}

func (d *Document) ProxyNameAvailable(name string) bool {
	return d.nameAvailable(name, -1) == nil
}

func (d *Document) backendReferences(name string) []string {
	var refs []string
	for _, s := range d.sections {
		if s.kind != SectionFrontend && s.kind != SectionListen {
			continue
		}
		for i := s.start + 1; i < s.end; i++ {
			fields := headerFields(d.lines[i].raw)
			if len(fields) >= 2 && (fields[0] == "default_backend" || fields[0] == "use_backend") && (fields[1] == name || strings.Contains(fields[1], "%[") || strings.ContainsAny(fields[1], "\"\\")) {
				refs = append(refs, s.name)
				break
			}
		}
	}
	return refs
}

func appendTimeout(lines []string, name, value string) []string {
	if value != "" {
		lines = append(lines, "\ttimeout "+name+" "+value)
	}
	return lines
}

func (d *Document) setDirective(s *section, path []string, value string) error {
	found := -1
	for i := s.start + 1; i < s.end; i++ {
		fields := headerFields(d.lines[i].raw)
		if len(fields) < len(path) {
			continue
		}
		matches := true
		for j := range path {
			if fields[j] != path[j] {
				matches = false
				break
			}
		}
		if !matches {
			continue
		}
		if found >= 0 {
			return fmt.Errorf("%w: duplicate %s", ErrAmbiguous, strings.Join(path, " "))
		}
		found = i
	}
	if value == "" {
		if found >= 0 {
			d.lines = append(d.lines[:found], d.lines[found+1:]...)
			s.end--
		}
		return nil
	}
	if found >= 0 {
		fields := fieldsAt(d.lines[found].raw)
		if len(fields) <= len(path) {
			return fmt.Errorf("%w: incomplete %s", ErrInvalid, strings.Join(path, " "))
		}
		fields[len(path)] = value
		d.lines[found].raw = []byte(string(indent(d.lines[found].raw)) + strings.Join(fields, " ") + string(lineEnding(d.lines[found].raw, d.newline)))
		return nil
	}
	fields := append(append([]string{}, path...), value)
	raw := []byte("\t" + strings.Join(fields, " ") + string(d.newline))
	d.insertLineRaw(s.start+1, raw)
	s.end++
	return nil
}

func (d *Document) appendSection(lines []string) {
	if len(d.lines) > 0 && !bytes.HasSuffix(d.lines[len(d.lines)-1].raw, []byte("\n")) {
		d.lines[len(d.lines)-1].raw = append(d.lines[len(d.lines)-1].raw, d.newline...)
	}
	for _, text := range lines {
		d.lines = append(d.lines, line{raw: []byte(text + string(d.newline)), section: -1})
	}
	d.reparse()
}

func (d *Document) insertLineRaw(at int, raw []byte) {
	d.lines = append(d.lines, line{})
	copy(d.lines[at+1:], d.lines[at:])
	d.lines[at] = line{raw: raw, section: -1}
}

func (d *Document) insertLine(at int, raw []byte) {
	d.insertLineRaw(at, raw)
	d.reparse()
}

func (d *Document) removeSection(index int) {
	s := d.sections[index]
	d.lines = append(d.lines[:s.start], d.lines[s.end:]...)
	d.reparse()
}

func (d *Document) reparse() {
	parsed, _ := Parse(d.Bytes())
	*d = *parsed
}

func replaceSectionName(raw []byte, name string, newline []byte) []byte {
	fields := fieldsAt(raw)
	fields[1] = name
	return []byte(string(indent(raw)) + strings.Join(fields, " ") + string(lineEnding(raw, newline)))
}

func renderServer(server Server, original []byte) string {
	endpoint := formatEndpoint(server.Address, server.Port)
	var options []string
	indentation := ""
	ending := ""
	var comment []string
	if original != nil {
		fields := fieldsAt(original)
		end := len(fields)
		for i, field := range fields {
			if strings.HasPrefix(field, "#") {
				end = i
				comment = fields[i:]
				break
			}
		}
		for i := 3; i < end; i++ {
			switch fields[i] {
			case "check", "no-check", "backup", "disabled":
				continue
			case "weight", "inter", "fall", "rise":
				if i+1 < end {
					i++
				}
				continue
			default:
				options = append(options, fields[i])
			}
		}
		indentation = string(indent(original))
		ending = string(lineEnding(original, []byte("\n")))
	}
	options = append(options, renderedServerOptions(server)...)
	result := []string{"server", server.Name, endpoint}
	result = append(result, options...)
	result = append(result, comment...)
	return indentation + strings.Join(result, " ") + ending
}

func renderedServerOptions(server Server) []string {
	var options []string
	if server.HealthCheck {
		options = append(options, "check")
	}
	if server.CheckDisabled {
		options = append(options, "no-check")
	}
	if server.Backup {
		options = append(options, "backup")
	}
	if server.Disabled {
		options = append(options, "disabled")
	}
	if server.WeightSet || server.Weight != 0 {
		options = append(options, "weight", strconv.Itoa(server.Weight))
	}
	if server.Interval != "" {
		options = append(options, "inter", server.Interval)
	}
	if server.Fall != 0 {
		options = append(options, "fall", strconv.Itoa(server.Fall))
	}
	if server.Rise != 0 {
		options = append(options, "rise", strconv.Itoa(server.Rise))
	}
	return options
}

func indent(raw []byte) []byte {
	end := 0
	for end < len(raw) && (raw[end] == ' ' || raw[end] == '\t') {
		end++
	}
	return raw[:end]
}

func lineEnding(raw, fallback []byte) []byte {
	if bytes.HasSuffix(raw, []byte("\r\n")) {
		return []byte("\r\n")
	}
	if bytes.HasSuffix(raw, []byte("\n")) {
		return []byte("\n")
	}
	return fallback
}

// Unsupported quoting, continuations and preprocessor scopes cannot be safely
// rewritten by the token-based editor; lossless reading remains available.
func (d *Document) ensureEditable() error {
	for _, l := range d.lines {
		raw := strings.TrimSpace(string(l.raw))
		if strings.HasPrefix(raw, "#") {
			continue
		}
		if strings.HasPrefix(raw, "default-server ") || strings.HasPrefix(raw, "server-template ") {
			return fmt.Errorf("%w: inherited or generated servers require manual editing", ErrAmbiguous)
		}
		if strings.HasPrefix(raw, ".if") || strings.HasPrefix(raw, ".else") || strings.HasPrefix(raw, ".elif") || strings.HasPrefix(raw, ".endif") || strings.ContainsAny(raw, "\\\"'") {
			return fmt.Errorf("%w: quoted, escaped or conditional configuration requires manual editing", ErrAmbiguous)
		}
	}
	return nil
}
