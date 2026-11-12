package haproxy

import "fmt"

// ReorderServers moves complete original server lines and leaves all other lines untouched.
func (d *Document) ReorderServers(backend string, names []string) error {
	if e := d.ensureEditable(); e != nil {
		return e
	}
	index, e := d.uniqueSection(SectionBackend, backend)
	if e != nil {
		return e
	}
	s := d.sections[index]
	lines := map[string][]byte{}
	var positions []int
	for i := s.start + 1; i < s.end; i++ {
		f := fieldsAt(d.lines[i].raw)
		if len(f) > 0 && f[0] == "server-template" {
			return fmt.Errorf("%w: server-template ordering", ErrAmbiguous)
		}
		if len(f) >= 3 && f[0] == "server" {
			if lines[f[1]] != nil {
				return ErrAmbiguous
			}
			lines[f[1]] = d.lines[i].raw
			positions = append(positions, i)
		}
	}
	if len(names) != len(positions) {
		return fmt.Errorf("%w: reorder requires every server", ErrInvalid)
	}
	seen := map[string]bool{}
	for _, name := range names {
		if seen[name] || lines[name] == nil {
			return fmt.Errorf("%w: reorder target %q", ErrInvalid, name)
		}
		seen[name] = true
	}
	for i, pos := range positions {
		d.lines[pos].raw = lines[names[i]]
	}
	return nil
}
