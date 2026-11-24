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
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type result struct {
	err          error
	text         string
	open         bool
	serverData   []haproxy.Server
	returnMode   string
	settingsData *settings.Settings
}
type statsMsg struct {
	snapshot monitor.Snapshot
	path     string
	err      error
}
type tick time.Time
type Model struct {
	s                        *app.Session
	cfg                      settings.Settings
	settingsPath             string
	mode, notice, filter     string
	cursor, width, height    int
	menuIndex, backupIndex   int
	fields                   []textinput.Model
	focus                    int
	fieldEditing             bool
	fieldBefore              string
	view                     viewport.Model
	servers                  []haproxy.Server
	copySource               string
	action                   func() tea.Msg
	busy                     bool
	stats                    monitor.Snapshot
	statsErr                 error
	detailOffset             int
	backups                  []backup.Metadata
	editing                  string
	search                   bool
	prefs                    []preference
	prefIndex, prefGroup     int
	serverFields             []textinput.Model
	serverIndex, serverFocus int
	serverNew                bool
	confirmReturn            string
}

func newLegacy(p string) Model {
	m := Model{settingsPath: p, width: 100, height: 30}
	m.view = viewport.New(m.width-4, m.height-9)
	m.view.SetHorizontalStep(8)
	s, e := settings.Load(p)
	if os.IsNotExist(e) {
		m.cfg = settings.Default()
		m.mode = "wizard"
		m.notice = "Первый запуск: настройте TOML. Ctrl+S — сохранить, Esc — выход."
		return m
	}
	if e != nil {
		m.cfg = settings.Default()
		m.mode = "settings"
		m.notice = e.Error()
		return m
	}
	m.cfg = s
	m.open()
	return m
}
func (m *Model) open() {
	m.stats, m.statsErr, m.detailOffset = monitor.Snapshot{}, nil, 0
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
	old, new := strings.Split(strings.TrimSuffix(string(a), "\n"), "\n"), strings.Split(strings.TrimSuffix(string(b), "\n"), "\n")
	prefix := 0
	for prefix < len(old) && prefix < len(new) && old[prefix] == new[prefix] {
		prefix++
	}
	suffix := 0
	for suffix < len(old)-prefix && suffix < len(new)-prefix && old[len(old)-1-suffix] == new[len(new)-1-suffix] {
		suffix++
	}
	start := max(0, prefix-3)
	oldEnd, newEnd := len(old)-max(0, suffix-3), len(new)-max(0, suffix-3)
	var out strings.Builder
	fmt.Fprintf(&out, "--- Текущая конфигурация\n+++ Изменения\n@@ -%d,%d +%d,%d @@\n", start+1, oldEnd-start, start+1, newEnd-start)
	for _, l := range old[start:prefix] {
		out.WriteString(" " + l + "\n")
	}
	for _, l := range old[prefix : len(old)-suffix] {
		out.WriteString("-" + l + "\n")
	}
	for _, l := range new[prefix : len(new)-suffix] {
		out.WriteString("+" + l + "\n")
	}
	for _, l := range new[len(new)-suffix : newEnd] {
		out.WriteString(" " + l + "\n")
	}
	return out.String()
}
func (m *Model) preview(text string, action func() tea.Msg) {
	m.confirmReturn = m.mode
	m.view.SetContent(text)
	m.view.GotoTop()
	m.view.SetXOffset(0)
	m.mode = "confirm"
	m.action = action
}
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if next, cmd, handled := m.formUpdate(msg); handled {
		return next, cmd
	}
	switch x := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = x.Width
		m.height = x.Height
		m.view.Width = max(20, x.Width-4)
		m.view.Height = max(3, x.Height-9)
		m.resizeRouteInputs()
		return m, nil
	case result:
		m.busy = false
		if x.settingsData != nil && x.err == nil {
			m.cfg = *x.settingsData
		}
		if x.serverData != nil {
			m.servers = x.serverData
			m.serverIndex = min(m.serverIndex, max(0, len(m.servers)-1))
		}
		if x.returnMode != "" {
			m.mode = x.returnMode
			m.notice = x.text
			return m, nil
		}
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
		if x.path == m.cfg.Monitoring.Socket {
			m.stats, m.statsErr = x.snapshot, x.err
		}
		return m, nil
	case tick:
		cmd := tea.Tick(time.Duration(max(1, m.cfg.UI.RefreshInterval))*time.Second, func(t time.Time) tea.Msg { return tick(t) })
		if path := m.cfg.Monitoring.Socket; m.s != nil && !m.busy && path != "" {
			return m, tea.Batch(cmd, func() tea.Msg {
				ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
				defer cancel()
				s, e := monitor.Read(ctx, path)
				return statsMsg{snapshot: s, path: path, err: e}
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
		if m.mode == "missing" {
			if key == "q" || key == "esc" {
				return m, tea.Quit
			}
			if key == "o" {
				m.mode = "settings"
				m.beginPreferences(false)
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
				m.beginPreferences(false)
			}
			return m, nil
		}
		if m.mode == "confirm" {
			if key == "y" {
				m.busy = true
				return m, m.action
			}
			if key == "n" || key == "q" || key == "esc" {
				m.mode = m.confirmReturn
				m.action = nil
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
				m.backupIndex = min(m.backupIndex+1, max(0, len(m.backups)-1))
			}
			if key == "k" || key == "up" {
				m.backupIndex = max(0, m.backupIndex-1)
			}
			if len(m.backups) > 0 && (key == "enter" || key == "r") {
				id := m.backups[m.backupIndex].ID
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
				if key == "esc" {
					m.filter = ""
				}
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
			m.detailOffset = 0
			return m, nil
		}
		rows := m.rows()
		if m.mode == "actions" {
			actions := m.routeActions(len(rows) > 0)
			switch key {
			case "esc", "?":
				m.mode = "main"
				return m, nil
			case "down", "j":
				m.menuIndex = min(m.menuIndex+1, len(actions)-1)
				return m, nil
			case "up", "k":
				m.menuIndex = max(0, m.menuIndex-1)
				return m, nil
			case "enter":
				key = actions[m.menuIndex].key
				m.mode = "main"
			default:
				return m, nil
			}
		}
		if len(rows) > 0 {
			m.cursor = min(max(0, m.cursor), len(rows)-1)
		} else {
			m.cursor = 0
		}
		switch key {
		case "enter":
			m.openRoute(editRoute)
		case "?", "tab":
			m.mode = "actions"
			m.menuIndex = 0
		case "esc":
			if m.filter != "" {
				m.filter = ""
				m.cursor = 0
				m.detailOffset = 0
			}
		case "q":
			return m, tea.Quit
		case "j", "down":
			m.detailOffset = 0
			m.cursor = min(m.cursor+1, max(0, len(rows)-1))
		case "k", "up":
			m.detailOffset = 0
			m.cursor = max(0, m.cursor-1)
		case "/":
			m.search = true
		case "a":
			m.openRoute(newRoute)
		case "e":
			m.openRoute(editRoute)
		case "c":
			m.openRoute(copyRoute)
		case "o":
			m.mode = "settings"
			m.beginPreferences(false)
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
			m.preview("Сохранить изменения в файл?\n"+diff(m.s.Original, m.s.Document.Bytes()), func() tea.Msg {
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
				m.backupIndex = 0
				m.mode = "backups"
			}
		case "m":
			m.busy = true
			return m, func() tea.Msg {
				b, e := m.s.Backups.Create(m.s.Path)
				return result{err: e, text: "Создан backup " + b.ID}
			}
		case "pgup", "pgdown":
			if len(rows) > 0 {
				_, _, _, height := m.routeLayout(len(rows), m.cursor)
				details := m.routeDetails(rows[m.cursor])
				if m.useDashboard() {
					width, _, listWidth, _ := m.dashboardDimensions()
					details = m.dashboardDetails(rows[m.cursor], width-listWidth-5)
				}
				last := max(0, len(details)-height)
				step := max(1, height-1)
				if key == "pgup" {
					step = -step
				}
				m.detailOffset = min(last, max(0, min(m.detailOffset, last)+step))
			}
		case "i":
			if len(rows) > 0 {
				f := rows[m.cursor]
				lines := append([]string{f.Name}, m.routeDetails(f)...)
				m.view.SetContent(strings.Join(lines, "\n"))
				m.view.GotoTop()
				m.view.SetXOffset(0)
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
func (m Model) connectionWarning(backend string) string {
	servers := m.stats.Servers[backend]
	if len(servers) == 0 {
		return "Активные соединения: UNKNOWN (нет статистики backend).\n"
	}
	var n int64
	for _, server := range servers {
		n += server.Current
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
