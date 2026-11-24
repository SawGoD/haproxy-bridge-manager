package tui

import (
	"fmt"
	"net"
	"strconv"
	"strings"

	"github.com/SawGoD/haproxy-bridge/internal/haproxy"
	tea "github.com/charmbracelet/bubbletea"
)

type routeOperation int

const (
	editRoute routeOperation = iota
	newRoute
	copyRoute
)

const routePortField = 3

var routeLabels = []string{"Frontend", "Backend", "Адрес прослушивания", "Локальный порт", "Таймаут соединения", "Таймаут клиента", "Таймаут сервера"}

func (m *Model) openRoute(operation routeOperation) {
	d := m.s.Document
	frontend := haproxy.Frontend{Bind: haproxy.Bind{Address: m.cfg.Network.DefaultBindAddress, Port: m.cfg.Network.DefaultStartPort}}
	var backend haproxy.Backend
	var editing, source, notice string
	if operation != newRoute {
		rows := m.rows()
		if len(rows) == 0 {
			return
		}
		frontend = rows[m.cursor]
		var err error
		backend, err = d.Backend(frontend.Backend)
		if err != nil {
			m.notice = err.Error()
			return
		}
		if operation == copyRoute {
			source = frontend.Name
			frontend.Name = copyName(d, frontend.Name, "")
			frontend.Backend = copyName(d, backend.Name, frontend.Name)
		} else {
			editing = frontend.Name
		}
	}
	port := strconv.Itoa(frontend.Bind.Port)
	if operation != editRoute {
		free, err := d.NextFreePort(frontend.Bind.Port)
		if err != nil {
			m.notice = err.Error()
			return
		}
		switch {
		case free == 0:
			port = ""
			notice = fmt.Sprintf("Нет свободных портов от %d. Введите другой порт.", frontend.Bind.Port)
		case free != frontend.Bind.Port:
			port = strconv.Itoa(free)
			notice = fmt.Sprintf("Порт %d занят. Предложен %d.", frontend.Bind.Port, free)
		}
	}
	m.editing, m.copySource, m.notice = editing, source, notice
	m.servers, m.serverIndex = backend.Servers, 0
	m.fields = nil
	for _, value := range []string{frontend.Name, frontend.Backend, frontend.Bind.Address, port, backend.TimeoutConnect, frontend.TimeoutClient, backend.TimeoutServer} {
		m.fields = append(m.fields, input(value))
	}
	m.fields[0].Placeholder = "имя frontend"
	m.fields[1].Placeholder = "имя backend"
	m.focus = 0
	m.fields[0].Focus()
	m.mode = "editor"
	m.resizeRouteInputs()
}

func copyName(d *haproxy.Document, source, reserved string) string {
	for i := 1; ; i++ {
		suffix := "_copy"
		if i > 1 {
			suffix += strconv.Itoa(i)
		}
		name := source[:min(len(source), 64-len(suffix))] + suffix
		if name != reserved && d.ProxyNameAvailable(name) {
			return name
		}
	}
}

func (m *Model) focusRouteField(index int) {
	m.fields[m.focus].Blur()
	m.focus = index
	m.fields[m.focus].Focus()
	m.mode = "editor"
}

func (m *Model) draftPort(applySuggestion bool) (int, error) {
	port, err := strconv.Atoi(strings.TrimSpace(m.fields[routePortField].Value()))
	if err != nil || haproxy.ValidatePort(port) != nil {
		m.focusRouteField(routePortField)
		return 0, fmt.Errorf("Локальный порт: введите число от 1 до 65535")
	}
	if m.editing != "" {
		return port, nil
	}
	free, err := m.s.Document.NextFreePort(port)
	if err != nil {
		return 0, err
	}
	if free == port {
		if applySuggestion {
			m.notice = fmt.Sprintf("Порт %d свободен в конфигурации.", port)
		}
		return port, nil
	}
	m.focusRouteField(routePortField)
	if free == 0 {
		return 0, fmt.Errorf("Порт %d занят. Выше свободных портов нет. Введите другой порт", port)
	}
	if !applySuggestion {
		return 0, fmt.Errorf("Порт %d занят. F3 — выбрать %d", port, free)
	}
	m.fields[routePortField].SetValue(strconv.Itoa(free))
	m.notice = fmt.Sprintf("Порт %d занят. Выбран %d.", port, free)
	return free, nil
}

func (m *Model) candidate() error {
	port, err := m.draftPort(false)
	if err != nil {
		return err
	}
	value := func(index int) string { return strings.TrimSpace(m.fields[index].Value()) }
	frontend := haproxy.Frontend{Name: value(0), Backend: value(1), Bind: haproxy.Bind{Address: value(2), Port: port}, TimeoutClient: value(5)}
	for index, name := range []string{frontend.Name, frontend.Backend} {
		if err := haproxy.ValidateIdentifier(name); err != nil {
			m.focusRouteField(index)
			return fmt.Errorf("%s: используйте латинские буквы, цифры, дефис или подчёркивание. Первый символ — буква", routeLabels[index])
		}
	}
	if err := haproxy.ValidateAddress(frontend.Bind.Address); err != nil {
		m.focusRouteField(2)
		return fmt.Errorf("Адрес прослушивания: %w", err)
	}
	if len(m.servers) == 0 {
		m.mode = "servers"
		return fmt.Errorf("Добавьте хотя бы один сервер клавишей A")
	}
	d, err := haproxy.Parse(m.s.Document.Bytes())
	if err != nil {
		return err
	}
	backend := haproxy.Backend{Name: frontend.Backend, Servers: m.servers, TimeoutConnect: value(4), TimeoutServer: value(6)}
	if m.copySource != "" {
		source, err := d.Frontend(m.copySource)
		if err != nil {
			return err
		}
		frontend.TimeoutServer = source.TimeoutServer
		if err := d.CopyRoute(m.copySource, frontend); err != nil {
			return err
		}
		if err := updateDraftBackend(d, frontend.Backend, backend); err != nil {
			return err
		}
	} else if m.editing == "" {
		if err := d.CreateBackend(backend); err != nil {
			return err
		}
		if err := d.CreateFrontend(frontend); err != nil {
			return err
		}
	} else {
		old, err := d.Frontend(m.editing)
		if err != nil {
			return err
		}
		frontend.TimeoutServer = old.TimeoutServer
		if old.Bind != frontend.Bind {
			listener, err := net.Listen("tcp", net.JoinHostPort(frontend.Bind.Address, strconv.Itoa(port)))
			if err != nil {
				m.focusRouteField(routePortField)
				return fmt.Errorf("Адрес прослушивания занят или недоступен: %w", err)
			}
			listener.Close()
		}
		if old.Backend != frontend.Backend {
			// Detach this frontend in the candidate so shared references still block the rename.
			detached := old
			detached.Backend = ""
			if err := d.UpdateFrontend(old.Name, detached); err != nil {
				return err
			}
		}
		if err := updateDraftBackend(d, old.Backend, backend); err != nil {
			return err
		}
		if old != frontend {
			if err := d.UpdateFrontend(old.Name, frontend); err != nil {
				return err
			}
		}
	}
	if err := d.Validate(); err != nil {
		return err
	}
	message := "Новый маршрут. Проверьте порт и список серверов.\n"
	if m.editing != "" {
		message = "Смена порта может нарушить внешние приложения. Изменение общего backend затрагивает его frontend.\n" + m.connectionWarning(frontend.Backend)
	}
	m.preview(message+diff(m.s.Document.Bytes(), d.Bytes()), func() tea.Msg {
		m.s.Document = d
		return result{text: "Изменения в памяти. S — записать в файл."}
	})
	return nil
}

func updateDraftBackend(d *haproxy.Document, name string, draft haproxy.Backend) error {
	old, err := d.Backend(name)
	if err != nil {
		return err
	}
	draft.TimeoutClient = old.TimeoutClient
	if old.Name != draft.Name || old.TimeoutConnect != draft.TimeoutConnect || old.TimeoutServer != draft.TimeoutServer {
		if err := d.UpdateBackend(name, draft); err != nil {
			return err
		}
	}
	existing := make(map[string]haproxy.Server, len(old.Servers))
	for _, server := range old.Servers {
		existing[server.Name] = server
	}
	order := make([]string, 0, len(draft.Servers))
	for _, server := range draft.Servers {
		previous, found := existing[server.Name]
		if !found {
			if err := d.CreateServer(draft.Name, server); err != nil {
				return err
			}
		} else if previous != server {
			if err := d.UpdateServer(draft.Name, server.Name, server); err != nil {
				return err
			}
		}
		delete(existing, server.Name)
		order = append(order, server.Name)
	}
	for _, server := range old.Servers {
		if _, removed := existing[server.Name]; removed {
			if err := d.DeleteServer(draft.Name, server.Name); err != nil {
				return err
			}
		}
	}
	return d.ReorderServers(draft.Name, order)
}
