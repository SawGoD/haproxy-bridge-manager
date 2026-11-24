package tui

import (
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/SawGoD/haproxy-bridge/internal/haproxy"
	"github.com/SawGoD/haproxy-bridge/internal/monitor"
)

func routeStatus(backend monitor.Backend) string {
	if backend.Status == "" {
		return "UNKNOWN"
	}
	if backend.OnBackup() {
		return "UP · резерв"
	}
	return backend.Status
}

func (m Model) routeDetails(route haproxy.Frontend) []string {
	backend := m.stats.Backends[route.Backend]
	servers := m.stats.Servers[route.Backend]
	status := routeStatus(backend)
	if backend.LastChange != nil {
		status += " · " + backend.LastChange.String()
	}
	lines := []string{accent.Bold(true).Render(route.Backend) + "  " + status}
	switch {
	case m.cfg.Monitoring.Socket == "":
		lines = append(lines, "Runtime API: сокет не настроен")
	case m.statsErr != nil:
		lines = append(lines, "Runtime API: "+oneLine(m.statsErr.Error()))
	case m.stats.Servers == nil:
		lines = append(lines, "Runtime API: ожидание статистики")
	case len(servers) == 0:
		lines = append(lines, "Runtime API: нет строк серверов")
	default:
		active := monitor.Active(servers)
		if len(active) == 0 {
			lines = append(lines, "Активных сессий нет")
		} else {
			lines = append(lines, "Обслуживают: "+strings.Join(active, ", "))
		}
	}
	for _, server := range servers {
		role := "осн."
		if server.Backup {
			role = "рез."
		}
		state := server.Status
		if state == "" {
			state = "UNKNOWN"
		}
		if strings.HasPrefix(state, "DOWN") && server.Check != "" {
			state += " · " + oneLine(server.Check)
		}
		if server.LastChange != nil {
			state += " · " + server.LastChange.String()
		}
		lines = append(lines, fmt.Sprintf("%s %-4s %s", server.Name, role, state))
	}
	if backend.Downtime != nil {
		lines = append(lines, "Простой backend: "+backend.Downtime.String())
	}
	lines = append(lines, "Вход: "+net.JoinHostPort(route.Bind.Address, strconv.Itoa(route.Bind.Port)))
	if len(servers) == 0 {
		if configured, err := m.s.Document.Backend(route.Backend); err == nil {
			for _, server := range configured.Servers {
				lines = append(lines, fmt.Sprintf("%s  %s", server.Name, net.JoinHostPort(server.Address, strconv.Itoa(server.Port))))
			}
		}
	}
	for _, server := range servers {
		lines = append(lines, accent.Render(server.Name),
			fmt.Sprintf("  Сессии: %d сейчас / %d всего · ошибки: %d", server.Current, server.Total, server.Errors))
		check := oneLine(server.Check)
		if check == "" {
			check = "нет данных"
		}
		if server.CheckDuration != nil {
			check += fmt.Sprintf(" · %d мс", *server.CheckDuration/time.Millisecond)
		}
		if backend.OnBackup() && !server.Backup && strings.HasPrefix(server.Status, "DOWN") {
			check = warning.Render(check)
		}
		lines = append(lines, "  Проверка: "+check)
		if reason := oneLine(server.CheckDetail); reason != "" {
			lines = append(lines, "  "+reason)
		}
	}
	return lines
}

func (m Model) routeLayout(count, cursor int) (listWidth, start, end, detailHeight int) {
	width, height := m.width-4, m.height-10
	if m.search || m.filter != "" {
		height--
	}
	height = max(3, height)
	listWidth, limit := width, max(1, height-5)
	if width >= 78 {
		listWidth, limit = width*2/5, height-1
	}
	start = max(0, cursor-limit+1)
	end = min(count, start+limit)
	detailHeight = height
	if listWidth == width {
		detailHeight -= end - start + 1
	}
	return listWidth, start, end, detailHeight
}

func oneLine(value string) string {
	return strings.TrimSpace(strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, value))
}
