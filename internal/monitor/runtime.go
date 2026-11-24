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
	Name, Status, Check, CheckDetail string
	Current, Total, Errors           int64
	LastChange, CheckDuration        *time.Duration
	Backup                           bool
}

type Backend struct {
	Status               string
	LastChange, Downtime *time.Duration
	Primary, Backup      int64
	ServerCountsKnown    bool
}

// OnBackup uses HAProxy's usable server counts for the whole backend.
func (b Backend) OnBackup() bool {
	return b.Status == "UP" && b.ServerCountsKnown && b.Primary == 0 && b.Backup > 0
}

type Snapshot struct {
	Backends map[string]Backend
	Servers  map[string][]Server
}

func Parse(data string) (Snapshot, error) {
	r := csv.NewReader(strings.NewReader(data))
	r.FieldsPerRecord = -1
	header, e := r.Read()
	if e != nil {
		return Snapshot{}, e
	}
	idx := map[string]int{}
	for i, h := range header {
		idx[strings.TrimSpace(strings.TrimPrefix(h, "# "))] = i
	}
	for _, field := range []string{"pxname", "svname", "status"} {
		if _, ok := idx[field]; !ok {
			return Snapshot{}, errors.New("invalid show stat header")
		}
	}
	get := func(row []string, k string) string {
		i, ok := idx[k]
		if !ok || i >= len(row) {
			return ""
		}
		return row[i]
	}
	out := Snapshot{Backends: make(map[string]Backend), Servers: make(map[string][]Server)}
	for {
		row, e := r.Read()
		if e == io.EOF {
			break
		}
		if e != nil {
			return Snapshot{}, e
		}
		backend, name := get(row, "pxname"), get(row, "svname")
		kind := get(row, "type")
		if backend == "" || name == "" || (kind == "" && name == "FRONTEND") {
			continue
		}
		status := get(row, "status")
		lastChange := duration(get(row, "lastchg"), time.Second)
		if kind == "1" || (kind == "" && name == "BACKEND") {
			primary, primaryErr := strconv.ParseInt(get(row, "act"), 10, 64)
			backup, backupErr := strconv.ParseInt(get(row, "bck"), 10, 64)
			out.Backends[backend] = Backend{Status: status, LastChange: lastChange,
				Downtime: duration(get(row, "downtime"), time.Second), Primary: primary, Backup: backup,
				ServerCountsKnown: primaryErr == nil && backupErr == nil}
			continue
		}
		if kind != "" && kind != "2" {
			continue
		}
		num := func(k string) int64 { v, _ := strconv.ParseInt(get(row, k), 10, 64); return v }
		out.Servers[backend] = append(out.Servers[backend], Server{Name: name, Status: status,
			Check: get(row, "check_status"), CheckDetail: get(row, "last_chk"),
			Current: num("scur"), Total: num("stot"), Errors: num("econ"), LastChange: lastChange,
			CheckDuration: duration(get(row, "check_duration"), time.Millisecond), Backup: get(row, "bck") == "1"})
	}
	return out, nil
}
func duration(value string, unit time.Duration) *time.Duration {
	n, err := strconv.ParseInt(value, 10, 64)
	if err != nil || n < 0 || n > int64((1<<63-1)/unit) {
		return nil
	}
	d := time.Duration(n) * unit
	return &d
}

func Read(ctx context.Context, path string) (Snapshot, error) {
	if path == "" {
		return Snapshot{}, errors.New("Runtime API unavailable: UNKNOWN")
	}
	c, e := (&net.Dialer{}).DialContext(ctx, "unix", path)
	if e != nil {
		return Snapshot{}, e
	}
	defer c.Close()
	deadline := time.Now().Add(3 * time.Second)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	c.SetDeadline(deadline)
	if _, e = io.WriteString(c, "show stat\n"); e != nil {
		return Snapshot{}, e
	}
	b, e := io.ReadAll(io.LimitReader(c, 8<<20))
	if e != nil {
		return Snapshot{}, e
	}
	return Parse(string(b))
}

// Active reports observed sessions, never a guessed routing decision.
func Active(servers []Server) []string {
	var names []string
	for _, s := range servers {
		if s.Current > 0 {
			names = append(names, s.Name)
		}
	}
	return names
}
