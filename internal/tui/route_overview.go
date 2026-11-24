package tui

import (
	"fmt"
	"net"
	"strconv"
	"strings"

	"github.com/SawGoD/haproxy-bridge/internal/haproxy"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var overviewTabs = []string{"Общее", "Серверы", "Параметры", "Статистика"}

func (m *Model) showDetails(mode, text string) {
	m.mode = mode
	m.view.SetContent(text)
	m.view.GotoTop()
	m.view.SetXOffset(0)
}

func (m *Model) previewApply(save bool) {
	method := m.cfg.Docker.ApplyMethod
	if m.cfg.HAProxy.Mode == "native" {
		method = m.cfg.Native.ApplyMethod
	}
	label := "Apply"
	if save {
		label = "Save & Apply"
	}
	m.preview(label+" · "+method+"\nЭта операция может прервать активные TCP-соединения.\n\n"+diff(m.s.Original, m.s.Document.Bytes()), func() tea.Msg {
		return startApply(m.s, save)
	})
}

func (m Model) startRouteProbe(route haproxy.Frontend, tls bool) (tea.Model, tea.Cmd) {
	var next tea.Cmd
	if backend, err := m.s.Document.Backend(route.Backend); err == nil && len(backend.Servers) > 0 {
		next = m.probeCmd(route, backend.Servers, tls)
	}
	m.busy = true
	return m, m.validateCmd(next)
}

func (m Model) updateOverview(key string) (tea.Model, tea.Cmd) {
	routes := m.rows()
	if len(routes) == 0 {
		m.mode = "main"
		return m, nil
	}
	route := routes[min(m.cursor, len(routes)-1)]
	backend, _ := m.s.Document.Backend(route.Backend)
	switch key {
	case "q", "esc":
		m.mode = "main"
	case "tab", "right":
		m.overviewTab = (m.overviewTab + 1) % len(overviewTabs)
		m.overviewOffset = 0
	case "shift+tab", "left":
		m.overviewTab = (m.overviewTab + len(overviewTabs) - 1) % len(overviewTabs)
		m.overviewOffset = 0
	case "up", "k", "down", "j":
		delta := 1
		if key == "up" || key == "k" {
			delta = -1
		}
		if m.overviewTab == 1 {
			m.overviewServer = min(max(0, len(backend.Servers)-1), max(0, m.overviewServer+delta))
		} else {
			m.overviewOffset = min(len(m.overviewRows(route, backend, m.overviewWidth()))-1, max(0, m.overviewOffset+delta))
		}
	case "enter":
		m.openRoute(editRoute)
		if m.mode == "editor" {
			m.editBack = "overview"
			if m.overviewTab == 1 && len(m.servers) > 0 {
				m.mode, m.serverIndex = "servers", min(m.overviewServer, len(m.servers)-1)
				m.openServer(false)
			}
		}
	case "t", "h":
		return m.startRouteProbe(route, key == "h")
	}
	return m, nil
}

func (m Model) overviewWidth() int {
	if m.width >= 86 {
		return m.width - 29
	}
	return m.width - 4
}

func sectionRows(names []string, index int, wide bool) []string {
	items := make([]string, len(names))
	for i, name := range names {
		if i == index {
			items[i] = dashboardSelection.Render("› " + name)
		} else if wide {
			items[i] = muted.Render("  " + name)
		} else {
			items[i] = muted.Render(name)
		}
	}
	if wide {
		return items
	}
	return []string{strings.Join(items, "  ")}
}

func (m Model) overviewView() string {
	routes := m.rows()
	help := hint("Tab", "разделы") + "   " + hint("↑↓", "выбор") + "   " + hint("Q / Esc", "назад") + "\n" +
		hint("Enter", "изменить") + "   " + hint("T / H", "проверить и тестировать")
	if len(routes) == 0 {
		return m.screen("Маршрут", "", "Маршрут недоступен", help)
	}
	route := routes[min(m.cursor, len(routes)-1)]
	backend, _ := m.s.Document.Backend(route.Backend)
	wide := m.width >= 86
	sections := sectionRows(overviewTabs, m.overviewTab, wide)
	width := m.overviewWidth()
	rows := m.overviewRows(route, backend, width)
	frameHeight := m.height - 9
	height := frameHeight - 1
	if wide {
		height = frameHeight - 2
	}
	if m.overviewTab == 1 && len(rows) > height {
		first := max(1, m.overviewServer-height+3)
		rows = append([]string{rows[0]}, rows[first:min(len(rows), first+height-1)]...)
	} else if len(rows) > height {
		at := min(m.overviewOffset, len(rows)-height)
		rows = rows[at : at+height]
	}
	if wide {
		columns := lipgloss.JoinHorizontal(lipgloss.Top, framedRows("Маршрут", sections, 20, frameHeight), " ",
			framedRows(overviewTabs[m.overviewTab], rows, width+4, frameHeight))
		return m.screen("Маршрут", route.Name, columns, help)
	}
	return m.screen("Маршрут", route.Name, strings.Join(append(sections, rows...), "\n"), help)
}

func (m Model) overviewRows(route haproxy.Frontend, backend haproxy.Backend, width int) []string {
	switch m.overviewTab {
	case 1:
		return m.serverTable(backend.Servers, backend.Name, m.overviewServer, width)
	case 2:
		return []string{property("Таймаут соединения", backend.TimeoutConnect), property("Таймаут клиента", route.TimeoutClient), property("Таймаут сервера", backend.TimeoutServer)}
	case 3:
		return m.routeDetails(route)
	default:
		status := routeStatus(m.stats.Backends[route.Backend])
		return []string{accent.Bold(true).Render(route.Name) + "  " + statusBadge(status), "",
			property("Frontend", route.Name), property("Backend", route.Backend), property("Адрес прослушивания", routeEndpoint(route)),
			property("Серверы", strconv.Itoa(len(backend.Servers))), "",
			m.routeSessionLine(m.stats.Backends[route.Backend], m.stats.Servers[route.Backend])}
	}
}

func property(label, value string) string {
	if value == "" {
		value = "—"
	}
	return muted.Render(cell(label, 22)) + normal.Render(value)
}

func (m Model) serverTable(servers []haproxy.Server, backend string, cursor, width int) []string {
	if len(servers) == 0 {
		return []string{"Серверов пока нет"}
	}
	nameWidth := min(20, width/3-2)
	wide := width >= 76
	roleWidth, statusWidth, additional := 10, 11, 0
	if wide {
		additional = 15
	}
	addressWidth := min(30, width-2-nameWidth-roleWidth-statusWidth-additional)
	columns := "  " + cell("Имя сервера", nameWidth) + cell("Адрес", addressWidth) + cell("Роль", roleWidth) + cell("Статус", statusWidth)
	if wide {
		columns += cell("Проверка", 11) + "Вес"
	}
	rows := []string{muted.Render(columns)}
	for i, server := range servers {
		role := "основной"
		if server.Backup {
			role = "резервный"
		}
		status, timing, weight := "UNKNOWN", "—", "—"
		for _, observed := range m.stats.Servers[backend] {
			if observed.Name == server.Name && observed.Status != "" {
				status, timing = observed.Status, statTime(observed.CheckDuration)
				break
			}
		}
		if server.Disabled {
			role = "выключен"
		}
		if server.WeightSet {
			weight = fmt.Sprint(server.Weight)
		}
		line := "  " + cell(server.Name, nameWidth) + cell(net.JoinHostPort(server.Address, strconv.Itoa(server.Port)), addressWidth) + cell(role, roleWidth)
		if i == cursor {
			line = dashboardSelection.Render("› " + line[2:])
		}
		line += cell(statusBadge(status), statusWidth)
		if wide {
			line += cell(timing, 11) + weight
		}
		rows = append(rows, line)
	}
	return rows
}
