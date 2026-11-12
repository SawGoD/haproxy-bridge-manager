package haproxy

import (
	"bytes"
	"errors"
	"fmt"
)

var (
	ErrNotFound  = errors.New("haproxy: target not found")
	ErrExists    = errors.New("haproxy: target already exists")
	ErrAmbiguous = errors.New("haproxy: ambiguous target")
	ErrInUse     = errors.New("haproxy: backend is referenced")
	ErrInvalid   = errors.New("haproxy: invalid value")
)

type SectionKind string

const (
	SectionGlobal    SectionKind = "global"
	SectionDefaults  SectionKind = "defaults"
	SectionFrontend  SectionKind = "frontend"
	SectionBackend   SectionKind = "backend"
	SectionListen    SectionKind = "listen"
	SectionPeers     SectionKind = "peers"
	SectionResolvers SectionKind = "resolvers"
	SectionUserlist  SectionKind = "userlist"
)

type line struct {
	raw     []byte
	section int
}

type section struct {
	kind  SectionKind
	name  string
	start int
	end   int
}

type Document struct {
	lines    []line
	sections []section
	newline  []byte
}

func Parse(data []byte) (*Document, error) {
	d := &Document{newline: []byte("\n")}
	for offset := 0; offset < len(data); {
		end := bytes.IndexByte(data[offset:], '\n')
		var raw []byte
		if end < 0 {
			raw = data[offset:]
			offset = len(data)
		} else {
			end += offset + 1
			raw = data[offset:end]
			offset = end
			if len(raw) >= 2 && raw[len(raw)-2] == '\r' {
				d.newline = []byte("\r\n")
			}
		}
		d.lines = append(d.lines, line{raw: bytes.Clone(raw), section: -1})
	}
	for i := range d.lines {
		fields := headerFields(d.lines[i].raw)
		if len(fields) == 0 {
			continue
		}
		kind := SectionKind(fields[0])
		var name string
		switch kind {
		case SectionGlobal:
			if len(fields) != 1 {
				return nil, malformedHeader(i, kind)
			}
		case SectionDefaults:
			if len(fields) > 2 {
				return nil, malformedHeader(i, kind)
			}
			if len(fields) == 2 {
				name = fields[1]
			}
		case SectionFrontend, SectionBackend, SectionListen, SectionPeers, SectionResolvers, SectionUserlist:
			if len(fields) != 2 {
				return nil, malformedHeader(i, kind)
			}
			name = fields[1]
		default:
			if len(fields) < 2 || hasIndent(d.lines[i].raw) {
				continue
			}
			name = fields[1]
		}
		d.sections = append(d.sections, section{kind: kind, name: name, start: i, end: len(d.lines)})
		if len(d.sections) > 1 {
			d.sections[len(d.sections)-2].end = i
		}
	}
	current := -1
	for i := range d.lines {
		for current+1 < len(d.sections) && d.sections[current+1].start == i {
			current++
		}
		d.lines[i].section = current
	}
	return d, nil
}

func malformedHeader(line int, kind SectionKind) error {
	return fmt.Errorf("%w: line %d has a malformed %s header", ErrInvalid, line+1, kind)
}

func (d *Document) Bytes() []byte {
	var out []byte
	for _, line := range d.lines {
		out = append(out, line.raw...)
	}
	return out
}

func (d *Document) clone() *Document {
	copyDoc, _ := Parse(d.Bytes())
	return copyDoc
}

func directiveFields(raw []byte) [][]byte {
	raw = bytes.TrimSuffix(raw, []byte("\n"))
	raw = bytes.TrimSuffix(raw, []byte("\r"))
	return bytes.Fields(raw)
}

func fieldsAt(raw []byte) []string {
	parts := directiveFields(raw)
	out := make([]string, len(parts))
	for i, part := range parts {
		out[i] = string(part)
	}
	return out
}

func headerFields(raw []byte) []string {
	fields := fieldsAt(raw)
	for i, field := range fields {
		if len(field) > 0 && field[0] == '#' {
			return fields[:i]
		}
	}
	return fields
}

func hasIndent(raw []byte) bool {
	return len(raw) > 0 && (raw[0] == ' ' || raw[0] == '\t')
}
