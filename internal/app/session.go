package app

import (
	"context"
	"errors"
	"fmt"
	"github.com/SawGoD/haproxy-bridge/internal/backup"
	"github.com/SawGoD/haproxy-bridge/internal/haproxy"
	"github.com/SawGoD/haproxy-bridge/internal/platform"
	"github.com/SawGoD/haproxy-bridge/internal/settings"
	"github.com/SawGoD/haproxy-bridge/internal/storage"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

type Session struct {
	Settings   settings.Settings
	Path, Hash string
	Original   []byte
	Document   *haproxy.Document
	Backups    *backup.Manager
	Runner     platform.Runner
	goodID     string
	LogFile    string
}

func Open(s settings.Settings) (*Session, error) {
	b, h, e := storage.Read(s.General.ConfigPath)
	if e != nil {
		return nil, e
	}
	d, e := haproxy.Parse(b)
	if e != nil {
		return nil, e
	}
	m, e := backup.NewLazyManager(s.General.BackupDir, s.General.MaxBackups)
	if e != nil {
		return nil, e
	}
	return &Session{Settings: s, Path: s.General.ConfigPath, Hash: h, Original: b, Document: d, Backups: m, Runner: platform.ExecRunner{}}, nil
}
func (s *Session) docker() platform.Docker {
	return platform.Docker{Runner: s.Runner, ContainerName: s.Settings.Docker.ContainerName, ComposeFile: s.Settings.Docker.ComposeFile, ServiceName: s.Settings.Docker.ServiceName}
}
func (s *Session) Validate(b []byte) error {
	d, e := haproxy.Parse(b)
	if e != nil {
		return e
	}
	if e = d.Validate(); e != nil {
		return e
	}
	f, e := os.CreateTemp(filepath.Dir(s.Path), ".hbm-validate-*")
	if e != nil {
		return e
	}
	defer os.Remove(f.Name())
	if _, e = f.Write(b); e != nil {
		f.Close()
		return e
	}
	if info, x := os.Stat(s.Path); x == nil {
		if e = f.Chmod(info.Mode().Perm()); e != nil {
			f.Close()
			return e
		}
	}
	if e = f.Close(); e != nil {
		return e
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(s.Settings.HAProxy.ValidationTimeout)*time.Second)
	defer cancel()
	if s.Settings.HAProxy.Mode == "docker" {
		return s.docker().Validate(ctx, s.Path, f.Name())
	}
	return (platform.Native{Runner: s.Runner, Binary: s.Settings.Native.Binary, Service: s.Settings.Native.ServiceName}).Validate(ctx, f.Name())
}
func (s *Session) Save() error {
	b := s.Document.Bytes()
	w := storage.Writer{Validator: storage.ValidatorFunc(s.Validate), Backup: s.Backups}
	if e := w.Write(s.Path, b, s.Hash); e != nil {
		s.Log("save", "failed")
		return e
	}
	s.Original = b
	s.Hash = storage.Hash(b)
	s.Log("save", "success")
	return nil
}
func (s *Session) Reload() error {
	b, h, e := storage.Read(s.Path)
	if e != nil {
		return e
	}
	d, e := haproxy.Parse(b)
	if e != nil {
		return e
	}
	s.Original = b
	s.Hash = h
	s.Document = d
	return nil
}
func (s *Session) Restore(id string) error {
	if e := s.Backups.Restore(id, s.Path, s.Hash, storage.ValidatorFunc(s.Validate)); e != nil {
		return e
	}
	s.Log("restore", id)
	return s.Reload()
}
func (s *Session) Apply() error {
	b, h, e := storage.Read(s.Path)
	if e != nil {
		return e
	}
	if h != s.Hash {
		return storage.ErrHashConflict
	}
	if storage.Hash(s.Document.Bytes()) != h {
		return errors.New("unsaved changes: save before apply")
	}
	if e = s.Validate(b); e != nil {
		return e
	}
	if s.Settings.HAProxy.Mode == "native" {
		ctx, cancel := context.WithTimeout(context.Background(), time.Duration(s.Settings.HAProxy.ValidationTimeout)*time.Second)
		defer cancel()
		if e = (platform.Native{Runner: s.Runner, Binary: s.Settings.Native.Binary, Service: s.Settings.Native.ServiceName}).CheckServiceConfig(ctx, s.Path); e != nil {
			return e
		}
	}
	if _, e = s.Backups.Create(s.Path); e != nil {
		return e
	}
	method := s.Settings.Docker.ApplyMethod
	if s.Settings.HAProxy.Mode == "native" {
		method = s.Settings.Native.ApplyMethod
	}
	if method == "manual" {
		return errors.New("manual: file saved, configuration NOT applied")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	apply := func() error {
		if s.Settings.HAProxy.Mode == "docker" {
			return s.docker().Apply(ctx, s.Path, method)
		}
		return (platform.Native{Runner: s.Runner, Binary: s.Settings.Native.Binary, Service: s.Settings.Native.ServiceName}).Apply(ctx, method)
	}
	e = apply()
	if e == nil {
		e = s.checkListeners(ctx)
	}
	if e != nil {
		s.Log("apply", "failed")
		if s.goodID == "" {
			return fmt.Errorf("apply failed: %w; no confirmed rollback point; backups available for reviewed restore", e)
		}
		cancel()
		ctx, cancel = context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		if r := s.Restore(s.goodID); r != nil {
			return fmt.Errorf("apply failed %v; rollback restore failed: %w", e, r)
		}
		if r := apply(); r != nil {
			return fmt.Errorf("apply failed %v; rollback start failed: %w", e, r)
		}
		if r := s.checkListeners(ctx); r != nil {
			return fmt.Errorf("apply failed %v; rollback verification failed: %w", e, r)
		}
		s.Log("rollback", "success")
		return fmt.Errorf("apply failed: %w; previous verified configuration restored", e)
	}
	m, e := s.Backups.Create(s.Path)
	if e != nil {
		return fmt.Errorf("applied but rollback point failed: %w", e)
	}
	if e = s.Backups.SetFlags(m.ID, true, true); e != nil {
		return e
	}
	s.goodID = m.ID
	s.Log("apply", "success")
	return nil
}
func (s *Session) checkListeners(ctx context.Context) error {
	frontends, e := s.Document.Frontends()
	if e != nil {
		return e
	}
	for _, f := range frontends {
		host := f.Bind.Address
		if host == "0.0.0.0" || host == "*" {
			host = "127.0.0.1"
		}
		if host == "::" {
			host = "::1"
		}
		c, e := (&net.Dialer{Timeout: time.Duration(s.Settings.Network.ConnectTimeout) * time.Second}).DialContext(ctx, "tcp", net.JoinHostPort(host, strconv.Itoa(f.Bind.Port)))
		if e != nil {
			return fmt.Errorf("frontend %s is unavailable: %w", f.Name, e)
		}
		c.Close()
	}
	return nil
}
func (s *Session) Log(action, result string) {
	if s.LogFile == "" {
		return
	}
	f, e := os.OpenFile(s.LogFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if e != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "%s %s %s\n", time.Now().Format(time.RFC3339), action, result)
}
