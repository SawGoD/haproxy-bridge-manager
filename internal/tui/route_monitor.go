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
	status = statusStyle(status).Bold(true).Render(status)
	if backend.LastChange != nil {
		status += " · " + backend.LastChange.String()
	}
	lines := []string{accent.Bold(true).Render(route.Backend) + "  " + status, m.routeSessionLine(backend, servers)}
	for _, server := range servers {
		role := "осн."
		if server.Backup {
			role = "рез."
		}
		state := server.Status
		if state == "" {
			state = "UNKNOWN"
		}
		state = statusStyle(state).Bold(true).Render(state)
		if strings.HasPrefix(server.Status, "DOWN") && server.Check != "" {
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
	lines = append(lines, "Вход: "+routeEndpoint(route))
	if len(servers) == 0 {
		if configured, err := m.s.Document.Backend(route.Backend); err == nil {
			for _, server := range configured.Servers {
				lines = append(lines, fmt.Sprintf("%s  %s", server.Name, net.JoinHostPort(server.Address, strconv.Itoa(server.Port))))
			}
		}
	}
	for _, server := range servers {
		lines = append(lines, accent.Render(server.Name), "  "+serverSessions(server), "  Проверка: "+serverCheck(server))
		if reason := oneLine(server.CheckDetail); reason != "" {
			lines = append(lines, "  "+reason)
		}
	}
	return lines
}

func (m Model) routeSessionLine(backend monitor.Backend, servers []monitor.Server) string {
	switch {
	case m.cfg.Monitoring.Socket == "":
		return "Runtime API: сокет не настроен"
	case m.statsErr != nil:
		return "Runtime API: " + oneLine(m.statsErr.Error())
	case m.stats.Servers == nil:
		return "Runtime API: ожидание статистики"
	case len(servers) == 0:
		return "Runtime API: нет строк серверов"
	default:
		active := monitor.Active(servers)
		if len(active) > 0 {
			return "Обслуживают: " + strings.Join(active, ", ")
		}
		if backend.Status == "UP" && backend.ServerCountsKnown {
			if backend.Primary > 0 {
				return "Нет сессий · доступен основной"
			}
			if backend.OnBackup() {
				return "Нет сессий · доступен резервный"
			}
		}
		return "Активных сессий нет"
	}
}

func routeEndpoint(route haproxy.Frontend) string {
	return net.JoinHostPort(route.Bind.Address, strconv.Itoa(route.Bind.Port))
}

func serverSessions(server monitor.Server) string {
	return fmt.Sprintf("Сессии: %d сейчас / %d всего · ошибки: %d", server.Current, server.Total, server.Errors)
}

func serverCheck(server monitor.Server) string {
	check := oneLine(server.Check)
	if check == "" {
		check = "нет данных"
	}
	if server.CheckDuration != nil {
		check += fmt.Sprintf(" · %d мс", *server.CheckDuration/time.Millisecond)
	}
	return check
}

func (m Model) routeLayout(count, cursor int) (listWidth, start, end, detailHeight int) {
	if m.useDashboard() {
		_, height, listWidth, _ := m.dashboardDimensions()
		limit := height - 4
		if m.search || m.filter != "" {
			limit--
		}
		start := max(0, cursor-limit+1)
		return listWidth, start, min(count, start+limit), height - 2
	}
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
