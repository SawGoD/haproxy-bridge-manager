// Package monitor reads optional HAProxy Runtime API statistics.
package monitor

import (
	"context"
	"encoding/csv"
	"errors"
	"io"
	"net"
	"strconv"
	"strings"
	"time"
)

type Server struct {
	Backend, Name, Status, Check string
	Current, Total, Errors       int64
	LastChange                   time.Duration
	Backup                       bool
}

func Parse(data string) ([]Server, error) {
	r := csv.NewReader(strings.NewReader(data))
	r.FieldsPerRecord = -1
	header, e := r.Read()
	if e != nil {
		return nil, e
	}
	idx := map[string]int{}
	for i, h := range header {
		idx[strings.TrimSpace(strings.TrimPrefix(h, "# "))] = i
	}
	if _, ok := idx["pxname"]; !ok {
		return nil, errors.New("invalid show stat header")
	}
	get := func(row []string, k string) string {
		i, ok := idx[k]
		if !ok || i >= len(row) {
			return ""
		}
		return row[i]
	}
	var out []Server
	for {
		row, e := r.Read()
		if e == io.EOF {
			break
		}
		if e != nil {
			return nil, e
		}
		name := get(row, "svname")
		if name == "FRONTEND" || name == "BACKEND" {
			continue
		}
		num := func(k string) int64 { v, _ := strconv.ParseInt(get(row, k), 10, 64); return v }
		out = append(out, Server{Backend: get(row, "pxname"), Name: name, Status: get(row, "status"), Check: get(row, "check_status"), Current: num("scur"), Total: num("stot"), Errors: num("econ"), LastChange: time.Duration(num("lastchg")) * time.Second, Backup: get(row, "bck") == "1"})
	}
	return out, nil
}
func Read(ctx context.Context, path string) ([]Server, error) {
	if path == "" {
		return nil, errors.New("Runtime API unavailable: UNKNOWN")
	}
	c, e := (&net.Dialer{}).DialContext(ctx, "unix", path)
	if e != nil {
		return nil, e
	}
	defer c.Close()
	deadline := time.Now().Add(3 * time.Second)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	c.SetDeadline(deadline)
	if _, e = io.WriteString(c, "show stat\n"); e != nil {
		return nil, e
	}
	b, e := io.ReadAll(io.LimitReader(c, 8<<20))
	if e != nil {
		return nil, e
	}
	return Parse(string(b))
}

// Active reports observed sessions, never a guessed routing decision.
func Active(servers []Server, backend string) string {
	var names []string
	for _, s := range servers {
		if s.Backend == backend && s.Current > 0 {
			names = append(names, s.Name)
		}
	}
	if len(names) == 0 {
		return "UNKNOWN"
	}
	return strings.Join(names, ",")
}
