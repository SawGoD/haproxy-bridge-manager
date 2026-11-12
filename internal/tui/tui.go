// Package tui contains only interaction and presentation; persistence lives in app.
package tui

import (
	"context"
	"fmt"
	"github.com/SawGoD/haproxy-bridge/internal/app"
	"github.com/SawGoD/haproxy-bridge/internal/backup"
	"github.com/SawGoD/haproxy-bridge/internal/haproxy"
	"github.com/SawGoD/haproxy-bridge/internal/monitor"
	"github.com/SawGoD/haproxy-bridge/internal/network"
	"github.com/SawGoD/haproxy-bridge/internal/platform"
	"github.com/SawGoD/haproxy-bridge/internal/settings"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type result struct {
	err  error
	text string
	open bool
}
type statsMsg struct {
	servers []monitor.Server
	err     error
}
type tick time.Time
type Model struct {
	s                     *app.Session
	cfg                   settings.Settings
	settingsPath          string
	mode, notice, filter  string
	cursor, width, height int
	fields                []textinput.Model
	focus                 int
	area                  textarea.Model
	view                  viewport.Model
	pending               *haproxy.Document
	action                func() tea.Msg
	busy                  bool
	stats                 []monitor.Server
	checked               time.Time
	backups               []backup.Metadata
	editing               string
	search                bool
}

func New(p string) Model {
	m := Model{settingsPath: p, width: 100, height: 30}
	m.view = viewport.New(90, 20)
	m.area = textarea.New()
	m.area.CharLimit = 0
	m.area.SetWidth(90)
	m.area.SetHeight(18)
	m.area.ShowLineNumbers = true
	s, e := settings.Load(p)
	if os.IsNotExist(e) {
		m.cfg = settings.Default()
		m.mode = "wizard"
		m.area.SetValue(m.cfg.String())
		m.area.Focus()
		m.notice = "Первый запуск: настройте TOML. Ctrl+S — сохранить, Esc — выход."
		return m
	}
	if e != nil {
		m.cfg = settings.Default()
		m.mode = "settings"
		m.area.SetValue(m.cfg.String())
		m.area.Focus()
		m.notice = e.Error()
		return m
	}
	m.cfg = s
	m.open()
	return m
}
func (m *Model) open() {
	s, e := app.Open(m.cfg)
	if e != nil {
		m.notice = e.Error()
		if os.IsNotExist(e) {
			m.mode = "missing"
		} else {
			m.mode = "error"
		}
		return
	}
	s.LogFile = filepath.Join(filepath.Dir(m.settingsPath), "actions.log")
	m.s = s
	m.mode = "main"
}
func (m Model) Init() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg { return tick(t) })
}
func (m Model) rows() []haproxy.Frontend {
	if m.s == nil {
		return nil
	}
	f, e := m.s.Document.Frontends()
	if e != nil {
		return nil
	}
	var out []haproxy.Frontend
	for _, x := range f {
		if strings.Contains(strings.ToLower(x.Name), strings.ToLower(m.filter)) {
			out = append(out, x)
		}
	}
	return out
}
func diff(a, b []byte) string {
	if string(a) == string(b) {
		return "Изменений нет"
	}
	var out strings.Builder
	out.WriteString("--- current\n+++ candidate\n@@ whole document @@\n")
	for _, l := range strings.Split(string(a), "\n") {
		out.WriteString("-" + l + "\n")
	}
	for _, l := range strings.Split(string(b), "\n") {
		out.WriteString("+" + l + "\n")
	}
	return out.String()
}
func (m *Model) preview(text string, action func() tea.Msg) {
	m.view.SetContent(text)
	m.view.GotoTop()
	m.mode = "confirm"
	m.action = action
}
func (m *Model) editor(add bool) {
	m.fields = nil
	m.focus = 0
	m.editing = ""
	values := []string{"", "", "", m.cfg.Network.DefaultBindAddress, strconv.Itoa(m.cfg.Network.DefaultStartPort), "", "", ""}
	if !add {
		rows := m.rows()
		if len(rows) == 0 {
			return
		}
		f := rows[m.cursor]
		m.editing = f.Name
		label := m.cfg.Names[f.Name]
		if label == "" {
			label = f.Name
		}
		values = []string{label, f.Name, f.Backend, f.Bind.Address, strconv.Itoa(f.Bind.Port), "", f.TimeoutClient, ""}
		b, e := m.s.Document.Backend(f.Backend)
		if e != nil {
			m.notice = e.Error()
			return
		}
		values[5] = b.TimeoutConnect
		values[7] = b.TimeoutServer
		var lines []string
		for _, s := range b.Servers {
			role := "primary"
			if s.Backup {
				role = "backup"
			}
			enabled := "enabled"
			if s.Disabled {
				enabled = "disabled"
			}
			check := "inherit"
			if s.HealthCheck {
				check = "check"
			} else if s.CheckDisabled {
				check = "nocheck"
			}
			weight := "-"
			if s.WeightSet || s.Weight != 0 {
				weight = strconv.Itoa(s.Weight)
			}
			interval := s.Interval
			if interval == "" {
				interval = "-"
			}
			fall, rise := "-", "-"
			if s.Fall != 0 {
				fall = strconv.Itoa(s.Fall)
			}
			if s.Rise != 0 {
				rise = strconv.Itoa(s.Rise)
			}
			lines = append(lines, fmt.Sprintf("%s %s %s %s %s %s %s %s %s", s.Name, net.JoinHostPort(s.Address, strconv.Itoa(s.Port)), role, weight, check, enabled, interval, fall, rise))
		}
		m.area.SetValue(strings.Join(lines, "\n"))
	} else {
		used := map[int]bool{}
		rows, _ := m.s.Document.Frontends()
		for _, f := range rows {
			used[f.Bind.Port] = true
		}
		for p := m.cfg.Network.DefaultStartPort; p <= 65535; p++ {
			if used[p] {
				continue
			}
			l, e := net.Listen("tcp", net.JoinHostPort(m.cfg.Network.DefaultBindAddress, strconv.Itoa(p)))
			if e == nil {
				l.Close()
				values[4] = strconv.Itoa(p)
				break
			}
		}
		check := "nocheck"
		if m.cfg.Healthcheck.Enabled {
			check = "check"
		}
		m.area.SetValue(fmt.Sprintf("server1 example.com:%d primary 1 %s enabled %ds %d %d", m.cfg.Network.DefaultTargetPort, check, m.cfg.Healthcheck.Interval, m.cfg.Healthcheck.Fall, m.cfg.Healthcheck.Rise))
	}
	for i, v := range values {
		f := textinput.New()
		f.Prompt = []string{"Name: ", "Frontend: ", "Backend: ", "Bind address: ", "Port: ", "Connect timeout: ", "Client timeout: ", "Server timeout: "}[i]
		f.SetValue(v)
		f.CharLimit = 253
		f.Width = 50
		m.fields = append(m.fields, f)
	}
	m.area.Blur()
	m.fields[0].Focus()
	m.mode = "editor"
}
func (m *Model) candidate() error {
	d, e := haproxy.Parse(m.s.Document.Bytes())
	if e != nil {
		return e
	}
	v := func(i int) string { return strings.TrimSpace(m.fields[i].Value()) }
	p, e := strconv.Atoi(v(4))
	if e != nil {
		return e
	}
	f := haproxy.Frontend{Name: v(1), Backend: v(2), Bind: haproxy.Bind{Address: v(3), Port: p}, TimeoutClient: v(6)}
	if m.editing != "" {
		old, e := d.Frontend(m.editing)
		if e != nil {
			return e
		}
		f.TimeoutServer = old.TimeoutServer
		if old.Bind != f.Bind {
			listener, err := net.Listen("tcp", net.JoinHostPort(f.Bind.Address, strconv.Itoa(f.Bind.Port)))
			if err != nil {
				return fmt.Errorf("новый bind занят или недоступен (в том числе возможен HAProxy): %w", err)
			}
			listener.Close()
		}
		if old.Backend != f.Backend {
			previous, err := d.Backend(old.Backend)
			if err != nil {
				return err
			}
			if err = d.UpdateFrontend(m.editing, f); err != nil {
				return err
			}
			previous.Name = f.Backend
			if err = d.UpdateBackend(old.Backend, previous); err != nil {
				return fmt.Errorf("backend rename blocked (check shared references): %w", err)
			}
		}
	}
	var servers []haproxy.Server
	for _, line := range strings.Split(m.area.Value(), "\n") {
		x := strings.Fields(line)
		if len(x) == 0 {
			continue
		}
		if len(x) != 9 {
			return fmt.Errorf("server: требуется 9 полей: name endpoint role weight check enabled interval fall rise")
		}
		host, port, e := net.SplitHostPort(x[1])
		if e != nil {
			return e
		}
		n, e := strconv.Atoi(port)
		if e != nil {
			return e
		}
		weight, e := optionalNumber(x[3])
		if e != nil {
			return e
		}
		fall, e := optionalNumber(x[7])
		if e != nil {
			return e
		}
		rise, e := optionalNumber(x[8])
		if e != nil {
			return e
		}
		if x[7] != "-" && fall < 1 || x[8] != "-" && rise < 1 {
			return fmt.Errorf("fall/rise must be positive or - for inheritance")
		}
		if x[2] != "primary" && x[2] != "backup" || x[4] != "check" && x[4] != "nocheck" && x[4] != "inherit" || x[5] != "enabled" && x[5] != "disabled" {
			return fmt.Errorf("invalid server role/check/state")
		}
		interval := x[6]
		if interval == "-" {
			interval = ""
		}
		servers = append(servers, haproxy.Server{Name: x[0], Address: host, Port: n, Backup: x[2] == "backup", Disabled: x[5] == "disabled", Weight: weight, HealthCheck: x[4] == "check", CheckDisabled: x[4] == "nocheck", WeightSet: x[3] != "-", Interval: interval, Fall: fall, Rise: rise})
	}
	if len(servers) == 0 {
		return fmt.Errorf("backend must have a server")
	}
	if m.editing == "" {
		if e = d.CreateBackend(haproxy.Backend{Name: f.Backend, Servers: servers, TimeoutConnect: v(5), TimeoutServer: v(7)}); e != nil {
			return e
		}
		e = d.CreateFrontend(f)
	} else {
		e = d.UpdateFrontend(m.editing, f)
		if e != nil {
			return e
		}
		old, err := d.Backend(f.Backend)
		if err != nil {
			return err
		}
		old.TimeoutConnect = v(5)
		old.TimeoutServer = v(7)
		if e = d.UpdateBackend(f.Backend, old); e != nil {
			return e
		}
		existing := map[string]bool{}
		wanted := map[string]bool{}
		for _, srv := range old.Servers {
			existing[srv.Name] = true
		}
		for _, srv := range servers {
			if wanted[srv.Name] {
				return fmt.Errorf("duplicate server %s", srv.Name)
			}
			wanted[srv.Name] = true
			if existing[srv.Name] {
				e = d.UpdateServer(f.Backend, srv.Name, srv)
			} else {
				e = d.CreateServer(f.Backend, srv)
			}
			if e != nil {
				return e
			}
		}
		for _, srv := range old.Servers {
			if !wanted[srv.Name] {
				if e = d.DeleteServer(f.Backend, srv.Name); e != nil {
					return e
				}
			}
		}
		names := []string{}
		for _, srv := range servers {
			names = append(names, srv.Name)
		}
		if e = d.ReorderServers(f.Backend, names); e != nil {
			return e
		}
	}
	if e != nil {
		return e
	}
	if e = d.Validate(); e != nil {
		return e
	}
	m.pending = d
	m.preview("Изменение порта может нарушить внешние приложения. Общий backend затрагивает все его frontend. Приоритет backups: один primary, без allbackups; иначе балансировка.\n"+m.connectionWarning(f.Backend)+diff(m.s.Document.Bytes(), d.Bytes()), func() tea.Msg {
		m.s.Document = d
		if m.cfg.Names == nil {
			m.cfg.Names = map[string]string{}
		}
		m.cfg.Names[f.Name] = v(0)
		if err := m.cfg.Save(m.settingsPath); err != nil {
			return result{err: err}
		}
		return result{text: "Изменения в памяти. S — сохранить после проверки HAProxy."}
	})
	return nil
}
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch x := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = x.Width
		m.height = x.Height
		m.view.Width = max(20, x.Width-4)
		m.view.Height = max(5, x.Height-8)
		m.area.SetWidth(max(20, x.Width-8))
		m.area.SetHeight(max(5, x.Height-16))
		return m, nil
	case result:
		m.busy = false
		m.notice = x.text
		if x.err != nil {
			m.notice = x.err.Error()
		}
		if len(m.notice) > 300 {
			m.view.SetContent(m.notice)
			m.view.GotoTop()
			m.mode = "view"
			return m, nil
		}
		if x.open && x.err == nil {
			m.open()
		} else if m.s != nil {
			m.mode = "main"
		}
		return m, nil
	case statsMsg:
		m.stats = x.servers
		m.checked = time.Now()
		if x.err != nil {
			m.stats = nil
		}
		return m, nil
	case tick:
		cmd := tea.Tick(time.Duration(max(1, m.cfg.UI.RefreshInterval))*time.Second, func(t time.Time) tea.Msg { return tick(t) })
		if m.s != nil && !m.busy {
			path := m.cfg.Monitoring.Socket
			return m, tea.Batch(cmd, func() tea.Msg {
				ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
				defer cancel()
				s, e := monitor.Read(ctx, path)
				return statsMsg{s, e}
			})
		}
		return m, cmd
	case tea.KeyMsg:
		if m.busy {
			return m, nil
		}
		key := x.String()
		if key == "ctrl+c" {
			return m, tea.Quit
		}
		if m.mode == "wizard" || m.mode == "settings" {
			if key == "esc" {
				if m.s == nil {
					return m, tea.Quit
				}
				m.mode = "main"
				return m, nil
			}
			if key == "ctrl+s" {
				s, e := settings.Parse(m.area.Value())
				if e == nil && m.s != nil && string(m.s.Document.Bytes()) != string(m.s.Original) {
					m.preview("Сохранение настроек перечитает файл и отменит несохранённые изменения.\n"+diff(m.s.Original, m.s.Document.Bytes()), func() tea.Msg { err := s.Save(m.settingsPath); return result{err: err, open: true} })
					return m, nil
				}
				if e == nil {
					e = s.Save(m.settingsPath)
				}
				if e != nil {
					m.notice = e.Error()
					return m, nil
				}
				m.cfg = s
				m.open()
				return m, nil
			}
			var cmd tea.Cmd
			m.area, cmd = m.area.Update(msg)
			return m, cmd
		}
		if m.mode == "missing" {
			if key == "q" || key == "esc" {
				return m, tea.Quit
			}
			if key == "o" {
				m.mode = "settings"
				m.area.SetValue(m.cfg.String())
				m.area.Focus()
				return m, nil
			}
			if key == "c" {
				m.preview("Файл отсутствует. Создать НОВУЮ минимальную TCP-конфигурацию по указанному пути? Существующий файл не будет заменён.", func() tea.Msg {
					e := os.MkdirAll(filepath.Dir(m.cfg.General.ConfigPath), 0750)
					if e != nil {
						return result{err: e}
					}
					f, e := os.OpenFile(m.cfg.General.ConfigPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0640)
					if e != nil {
						return result{err: e}
					}
					_, e = f.WriteString("global\n    maxconn 4096\ndefaults\n    mode tcp\n    timeout connect 5s\n    timeout client 1h\n    timeout server 1h\n")
					if e == nil {
						e = f.Sync()
					}
					f.Close()
					return result{err: e, open: true}
				})
			}
			return m, nil
		}
		if m.mode == "error" {
			if key == "q" || key == "esc" {
				return m, tea.Quit
			}
			if key == "o" {
				m.mode = "settings"
				m.area.SetValue(m.cfg.String())
				m.area.Focus()
			}
			return m, nil
		}
		if m.mode == "editor" {
			if key == "esc" {
				m.mode = "main"
				return m, nil
			}
			if key == "ctrl+s" {
				if e := m.candidate(); e != nil {
					m.notice = e.Error()
				}
				return m, nil
			}
			if key == "tab" {
				if m.focus < len(m.fields) {
					m.fields[m.focus].Blur()
				} else {
					m.area.Blur()
				}
				m.focus = (m.focus + 1) % (len(m.fields) + 1)
				if m.focus < len(m.fields) {
					m.fields[m.focus].Focus()
				} else {
					m.area.Focus()
				}
				return m, nil
			}
			var cmd tea.Cmd
			if m.focus < len(m.fields) {
				m.fields[m.focus], cmd = m.fields[m.focus].Update(msg)
			} else {
				m.area, cmd = m.area.Update(msg)
			}
			return m, cmd
		}
		if m.mode == "confirm" {
			if key == "y" {
				m.busy = true
				return m, m.action
			}
			if key == "n" || key == "esc" {
				if m.s == nil {
					m.mode = "missing"
				} else {
					m.mode = "main"
				}
				return m, nil
			}
			var cmd tea.Cmd
			m.view, cmd = m.view.Update(msg)
			return m, cmd
		}
		if m.mode == "view" {
			if key == "esc" {
				m.mode = "main"
				return m, nil
			}
			var cmd tea.Cmd
			m.view, cmd = m.view.Update(msg)
			return m, cmd
		}
		if m.mode == "backups" {
			if key == "esc" {
				m.mode = "main"
				return m, nil
			}
			if key == "j" || key == "down" {
				m.cursor = min(m.cursor+1, len(m.backups)-1)
			}
			if key == "k" || key == "up" {
				m.cursor = max(0, m.cursor-1)
			}
			if len(m.backups) > 0 && (key == "enter" || key == "r") {
				id := m.backups[m.cursor].ID
				b, e := m.s.Backups.Content(id)
				if e != nil {
					m.notice = e.Error()
					return m, nil
				}
				m.preview("Restore записывает файл после проверки и backup текущего. Apply отдельно.\n"+diff(m.s.Original, b), func() tea.Msg {
					return result{err: m.s.Restore(id), text: "Backup восстановлен; Apply отдельно"}
				})
			}
			return m, nil
		}
		if m.search {
			if key == "esc" || key == "enter" {
				m.search = false
				return m, nil
			}
			if key == "backspace" {
				r := []rune(m.filter)
				if len(r) > 0 {
					m.filter = string(r[:len(r)-1])
				}
			} else if len(x.Runes) > 0 {
				m.filter += string(x.Runes)
			}
			m.cursor = 0
			return m, nil
		}
		rows := m.rows()
		if len(rows) > 0 {
			m.cursor = min(max(0, m.cursor), len(rows)-1)
		} else {
			m.cursor = 0
		}
		switch key {
		case "q":
			return m, tea.Quit
		case "j", "down":
			m.cursor = min(m.cursor+1, max(0, len(rows)-1))
		case "k", "up":
			m.cursor = max(0, m.cursor-1)
		case "/":
			m.search = true
		case "a":
			m.editor(true)
		case "e":
			m.editor(false)
		case "o":
			m.mode = "settings"
			m.area.SetValue(m.cfg.String())
			m.area.Focus()
		case "d":
			if len(rows) > 0 {
				f := rows[m.cursor]
				d, _ := haproxy.Parse(m.s.Document.Bytes())
				if e := d.DeleteFrontend(f.Name); e != nil {
					m.notice = e.Error()
					break
				}
				note := "Удалить frontend " + f.Name + ". Backend " + f.Backend + " сохранён, чтобы не нарушить ссылки.\n"
				m.preview(note+diff(m.s.Document.Bytes(), d.Bytes()), func() tea.Msg {
					m.s.Document = d
					return result{text: "Frontend удалён из кандидата; S — сохранить"}
				})
			}
		case "s":
			m.preview(diff(m.s.Original, m.s.Document.Bytes()), func() tea.Msg {
				return result{err: m.s.Save(), text: "Сохранено после валидации; Apply отдельно"}
			})
		case "p":
			m.preview("Apply: restart/recreate могут оборвать активные TCP-соединения. При сбое возможен rollback только к подтверждённой рабочей точке.\n"+diff(m.s.Original, m.s.Document.Bytes()), func() tea.Msg {
				return result{err: m.s.Apply(), text: "Применено, содержимое и frontend проверены"}
			})
		case "x":
			m.preview("Save & Apply: возможен разрыв активных TCP-соединений.\n"+diff(m.s.Original, m.s.Document.Bytes()), func() tea.Msg {
				if e := m.s.Save(); e != nil {
					return result{err: e}
				}
				return result{err: m.s.Apply(), text: "Сохранено и применено"}
			})
		case "v":
			m.busy = true
			return m, func() tea.Msg {
				return result{err: m.s.Validate(m.s.Document.Bytes()), text: "Кандидат прошёл haproxy -c"}
			}
		case "r":
			m.preview("Перечитать файл с диска? Несохранённые изменения будут потеряны.", func() tea.Msg { return result{err: m.s.Reload(), text: "Файл перечитан"} })
		case "b":
			list, e := m.s.Backups.List()
			if e != nil {
				m.notice = e.Error()
			} else {
				m.backups = list
				m.cursor = 0
				m.mode = "backups"
			}
		case "m":
			m.busy = true
			return m, func() tea.Msg {
				b, e := m.s.Backups.Create(m.s.Path)
				return result{err: e, text: "Создан backup " + b.ID}
			}
		case "i":
			if len(rows) > 0 {
				f := rows[m.cursor]
				var text strings.Builder
				text.WriteString("Runtime API: наблюдаемые серверы, не прогноз следующего соединения\n")
				for _, srv := range m.stats {
					if srv.Backend == f.Backend {
						fmt.Fprintf(&text, "%s status=%s current=%d total=%d errors=%d check=%s last_change=%s ago backup=%v\n", srv.Name, srv.Status, srv.Current, srv.Total, srv.Errors, srv.Check, srv.LastChange, srv.Backup)
					}
				}
				if len(m.stats) == 0 {
					text.WriteString("UNKNOWN: Runtime API недоступен")
				}
				m.view.SetContent(text.String())
				m.mode = "view"
			}
		case "g":
			if m.cfg.HAProxy.Mode == "docker" {
				m.busy = true
				return m, func() tea.Msg {
					ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
					defer cancel()
					d := platform.Docker{Runner: m.s.Runner, ContainerName: m.cfg.Docker.ContainerName}
					text, err := d.Logs(ctx)
					if err != nil {
						return result{err: err}
					}
					return result{text: text}
				}
			}
		case "l":
			p, _ := settings.Path()
			b, e := os.ReadFile(filepath.Join(filepath.Dir(p), "actions.log"))
			if e != nil {
				m.notice = e.Error()
			} else {
				m.mode = "view"
				m.view.SetContent(string(b))
				m.view.GotoBottom()
			}
		case "t", "h":
			if len(rows) > 0 {
				f := rows[m.cursor]
				b, e := m.s.Document.Backend(f.Backend)
				if e != nil {
					m.notice = e.Error()
					break
				}
				m.busy = true
				tlsEnabled := key == "h"
				return m, func() tea.Msg {
					ctx, cancel := context.WithTimeout(context.Background(), time.Duration(m.cfg.Network.ConnectTimeout)*time.Second)
					defer cancel()
					type item struct {
						i int
						r network.Result
					}
					ch := make(chan item, len(b.Servers))
					for i, s := range b.Servers {
						go func(i int, s haproxy.Server) {
							ch <- item{i, network.Probe(ctx, net.JoinHostPort(s.Address, strconv.Itoa(s.Port)), m.cfg.Network.SNI, tlsEnabled)}
						}(i, s)
					}
					results := make([]network.Result, len(b.Servers))
					for range b.Servers {
						x := <-ch
						results[x.i] = x.r
					}
					return result{text: fmt.Sprintf("TCP connect time (не ICMP). VLESS+REALITY авторизация не проверяется.\n%+v", results)}
				}
			}
		}
		return m, nil
	}
	return m, nil
}
func (m Model) View() string {
	title := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("39")).Render("HAProxy Bridge Manager")
	if m.busy {
		return title + "\nВыполняется операция…\n" + m.notice
	}
	switch m.mode {
	case "wizard", "settings":
		return title + "\nSettings / TOML — Ctrl+S сохранить, Esc назад\n" + m.area.View() + "\n" + m.notice
	case "missing":
		return title + "\n" + m.notice + "\n[C] Создать новый минимальный файл  [O] Settings  [Q] Quit"
	case "error":
		return title + "\n" + m.notice + "\n[O] Settings [Q] Quit"
	case "editor":
		var b strings.Builder
		b.WriteString(title + "\nNode editor — Tab: следующее поле, Ctrl+S: preview, Esc: отмена\n")
		for _, f := range m.fields {
			b.WriteString(f.View() + "\n")
		}
		b.WriteString("Servers: name endpoint primary|backup weight check|nocheck enabled|disabled interval fall rise\n")
		b.WriteString(m.area.View() + "\n" + m.notice)
		return b.String()
	case "confirm":
		return title + "\n" + m.view.View() + "\n[Y] Подтвердить [N/Esc] Отмена; ↑↓ прокрутка"
	case "view":
		return title + "\n" + m.view.View() + "\nEsc — назад"
	case "backups":
		var b strings.Builder
		b.WriteString(title + "\nBackups — Enter/R: diff и Restore; Esc: назад\n")
		for i := max(0, m.cursor-max(1, m.height-8)+1); i < len(m.backups) && i < max(0, m.cursor-max(1, m.height-8)+1)+max(1, m.height-8); i++ {
			x := m.backups[i]
			mark := "  "
			if i == m.cursor {
				mark = "> "
			}
			b.WriteString(fmt.Sprintf("%s%s  %d bytes SHA256 %s pinned=%v good=%v\n", mark, x.ID, x.Size, x.SHA256, x.Pinned, x.Good))
		}
		return b.String()
	}
	host, _ := os.Hostname()
	var b strings.Builder
	b.WriteString(title + "\nHost: " + host + "   Search: " + m.filter + "\n")
	compact := m.width < 125
	if compact {
		b.WriteString("NODE / FRONTEND    PORT   ACTIVE           STATUS   CONNS  CHECKED\n")
	} else {
		b.WriteString("NODE / FRONTEND                      PORT   BACKEND           ACTIVE           STATUS   CONNS  CHECKED\n")
	}
	rows := m.rows()
	start := max(0, m.cursor-max(1, m.height-12)+1)
	end := min(len(rows), start+max(1, m.height-12))
	for i := start; i < end; i++ {
		f := rows[i]
		status := "UNKNOWN"
		var current int64
		for _, s := range m.stats {
			if s.Backend == f.Backend {
				if status == "UNKNOWN" {
					status = s.Status
				} else if status != s.Status {
					status = "MIXED"
				}
				current += s.Current
			}
		}
		mark := " "
		if i == m.cursor {
			mark = ">"
		}
		when := "—"
		if !m.checked.IsZero() {
			when = m.checked.Format("15:04:05")
		}
		connections := "—"
		if status != "UNKNOWN" {
			connections = strconv.FormatInt(current, 10)
		}
		if compact {
			b.WriteString(fmt.Sprintf("%s%-17s %-6d %-16s %-8s %-6s %s\n", mark, short(nodeLabel(m.cfg, f.Name), 17), f.Bind.Port, short(monitor.Active(m.stats, f.Backend), 16), short(status, 8), connections, when))
		} else {
			b.WriteString(fmt.Sprintf("%s%-35s %-6d %-17s %-16s %-8s %-6s %s\n", mark, nodeLabel(m.cfg, f.Name), f.Bind.Port, f.Backend, monitor.Active(m.stats, f.Backend), status, connections, when))
		}
	}
	if compact && len(rows) > 0 {
		f := rows[min(m.cursor, len(rows)-1)]
		b.WriteString(fmt.Sprintf("Bind: %s:%d  Backend: %s\n", f.Bind.Address, f.Bind.Port, f.Backend))
	}
	b.WriteString("\n[A] Add [E] Edit [D] Delete [/] Search [T] TCP [H] TLS [V] Validate\n[S] Save [P] Apply [X] Save&Apply [B] Backups [M] Backup [O] Settings\n[L] Logs [G] Docker logs [I] Runtime details [R] Reload [Q] Quit  ↑↓/j/k\n")
	b.WriteString(m.notice)
	return lipgloss.NewStyle().MaxWidth(max(20, m.width)).Render(b.String())
}

func nodeLabel(s settings.Settings, name string) string {
	if label := s.Names[name]; label != "" {
		return label + " / " + name
	}
	return name
}

func (m Model) connectionWarning(backend string) string {
	if len(m.stats) == 0 {
		return "Активные соединения: UNKNOWN (Runtime API недоступен).\n"
	}
	var n int64
	for _, srv := range m.stats {
		if srv.Backend == backend {
			n += srv.Current
		}
	}
	return fmt.Sprintf("Серверы backend обслуживают %d соединений.\n", n)
}

func optionalNumber(s string) (int, error) {
	if s == "-" {
		return 0, nil
	}
	return strconv.Atoi(s)
}

func short(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n-1]) + "…"
	}
	return s
}
