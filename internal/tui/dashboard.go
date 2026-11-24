package tui

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/SawGoD/haproxy-bridge/internal/haproxy"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

var dashboardBorder = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#717FA5", Dark: "#7483B3"})
var dashboardSelection = normal.Background(lipgloss.AdaptiveColor{Light: "#C4EEEE", Dark: "#164D55"}).Bold(true)
var statusUp = lipgloss.NewStyle().Foreground(lipgloss.CompleteAdaptiveColor{
	Light: lipgloss.CompleteColor{TrueColor: "#145C34", ANSI256: "22", ANSI: "0"},
	Dark:  lipgloss.CompleteColor{TrueColor: "#9AF3B3", ANSI256: "120", ANSI: "15"},
}).Background(lipgloss.CompleteAdaptiveColor{
	Light: lipgloss.CompleteColor{TrueColor: "#D8F4DD", ANSI256: "194", ANSI: "10"},
	Dark:  lipgloss.CompleteColor{TrueColor: "#143926", ANSI256: "22", ANSI: "2"},
})
var statusDown = lipgloss.NewStyle().Foreground(lipgloss.CompleteAdaptiveColor{
	Light: lipgloss.CompleteColor{TrueColor: "#992A38", ANSI256: "88", ANSI: "0"},
	Dark:  lipgloss.CompleteColor{TrueColor: "#FFC0C6", ANSI256: "217", ANSI: "15"},
}).Background(lipgloss.CompleteAdaptiveColor{
	Light: lipgloss.CompleteColor{TrueColor: "#FDE0E2", ANSI256: "224", ANSI: "9"},
	Dark:  lipgloss.CompleteColor{TrueColor: "#4C2029", ANSI256: "52", ANSI: "1"},
})
var statusWarning = lipgloss.NewStyle().Foreground(lipgloss.CompleteAdaptiveColor{
	Light: lipgloss.CompleteColor{TrueColor: "#77470D", ANSI256: "94", ANSI: "0"},
	Dark:  lipgloss.CompleteColor{TrueColor: "#F5CF89", ANSI256: "222", ANSI: "15"},
}).Background(lipgloss.CompleteAdaptiveColor{
	Light: lipgloss.CompleteColor{TrueColor: "#FFF0CB", ANSI256: "230", ANSI: "11"},
	Dark:  lipgloss.CompleteColor{TrueColor: "#493417", ANSI256: "58", ANSI: "3"},
})
var statusUnknown = lipgloss.NewStyle().Foreground(lipgloss.CompleteAdaptiveColor{
	Light: lipgloss.CompleteColor{TrueColor: "#505B70", ANSI256: "239", ANSI: "0"},
	Dark:  lipgloss.CompleteColor{TrueColor: "#B8C0D0", ANSI256: "250", ANSI: "15"},
}).Background(lipgloss.CompleteAdaptiveColor{
	Light: lipgloss.CompleteColor{TrueColor: "#EBEDF2", ANSI256: "254", ANSI: "7"},
	Dark:  lipgloss.CompleteColor{TrueColor: "#272D3D", ANSI256: "236", ANSI: "8"},
})

func statusStyle(label string) lipgloss.Style {
	switch {
	case label == "UP", label == "OPEN":
		return statusUp
	case strings.HasPrefix(label, "DOWN"):
		return statusDown
	case strings.HasPrefix(label, "UP "), strings.HasPrefix(label, "MAINT"), label == "DRAIN", label == "NOLB":
		return statusWarning
	default:
		return statusUnknown
	}
}

func statusBadge(label string) string {
	return statusStyle(label).Bold(true).Padding(0, 1).Render(label)
}

func cell(text string, width int) string {
	text = ansi.Truncate(text, width, "…")
	return text + strings.Repeat(" ", width-ansi.StringWidth(text))
}

func framedRows(title string, rows []string, width, height int) string {
	if title != "" {
		title = " " + ansi.Truncate(title, width-6, "…") + " "
	}
	line := func(s string) string { return dashboardBorder.Render(s) }
	top := line("╭─") + accent.Bold(true).Render(title) + line(strings.Repeat("─", width-3-ansi.StringWidth(title))+"╮")
	out := []string{top}
	for i := 0; i < height-2; i++ {
		value := ""
		if i < len(rows) {
			value = rows[i]
		}
		out = append(out, line("│")+" "+cell(value, width-4)+" "+line("│"))
	}
	out = append(out, line("╰"+strings.Repeat("─", width-2)+"╯"))
	return strings.Join(out, "\n")
}

func (m Model) useDashboard() bool { return m.width >= 100 && m.height >= 26 }

func dashboardKeys(width int) []string {
	actions := [][2]string{{"A", "Добавить"}, {"C", "Копия"}, {"D", "Удалить"}, {"T", "Проверка и тест"},
		{"S", "Сохранить"}, {"P", "Применить"}, {"Q", "Выход"}}
	keys := make([]string, 0, len(actions))
	for _, action := range actions {
		keys = append(keys, accent.Bold(true).Render("["+action[0]+"]")+" "+normal.Render(action[1]))
	}
	sep := dashboardBorder.Render(" │ ")
	rows := []string{strings.Join(keys, sep)}
	if ansi.StringWidth(rows[0]) > width {
		middle := len(keys) / 2
		rows = []string{strings.Join(keys[:middle], sep), strings.Join(keys[middle:], sep)}
	}
	return append(rows, hint("↑↓", "маршрут")+"   "+hint("Enter", "обзор")+"   "+hint("Pg↑↓", "детали")+"   "+hint("/", "поиск")+"   "+hint("Tab", "меню"))
}

func (m Model) dashboardDimensions() (width, height, listWidth int, footer []string) {
	width = m.width - 2
	footer = dashboardKeys(width - 4)
	if m.notice != "" {
		footer = append([]string{warning.Render(oneLine(m.notice))}, footer...)
	}
	height = m.height - len(footer) - 7
	if m.sidebarWidth() == 0 {
		height--
	}
	return width, height, (width - m.sidebarWidth()) * 35 / 100, footer
}

func (m Model) sidebarWidth() int {
	if m.width >= 144 {
		return 23
	}
	return 0
}

func (m Model) dashboardDetailWidth(width, listWidth int) int {
	if sidebar := m.sidebarWidth(); sidebar > 0 {
		width -= sidebar + 1
	}
	return width - listWidth - 1
}

func (m Model) dashboardNavigation() []string {
	items := []string{dashboardSelection.Render("› Маршруты"), hint("F2", "Серверы"),
		hint("I", "Статистика"), hint("B", "Бэкапы")}
	if m.cfg.HAProxy.Mode == "docker" {
		items = append(items, hint("G", "Контейнер"))
	}
	return append(items, hint("O", "Настройки"), hint("L", "Журнал"))
}

func (m Model) dashboardView(rows []haproxy.Frontend) string {
	width, height, listWidth, footer := m.dashboardDimensions()
	detailWidth := m.dashboardDetailWidth(width, listWidth)
	up, reserve, down, unknown := 0, 0, 0, 0
	for _, route := range rows {
		backend := m.stats.Backends[route.Backend]
		switch backend.Status {
		case "UP":
			up++
		case "DOWN":
			down++
		case "", "UNKNOWN":
			unknown++
		}
		if backend.OnBackup() {
			reserve++
		}
	}
	label := fmt.Sprintf("%d/%d UP", up, len(rows))
	style := statusWarning
	if up == len(rows) && reserve == 0 && up > 0 {
		style = statusUp
	}
	if reserve > 0 {
		label += fmt.Sprintf(" · резерв %d", reserve)
	}
	if unknown > 0 {
		label += fmt.Sprintf(" · UNKNOWN %d", unknown)
	}
	if down > 0 {
		label += fmt.Sprintf(" · DOWN %d", down)
		style = statusDown
	} else if unknown == len(rows) {
		style = statusUnknown
	}
	if len(rows) == 0 {
		label = "Нет маршрутов"
	}
	mode := m.cfg.HAProxy.Mode
	if mode == "docker" && m.cfg.Docker.ContainerName != "" {
		mode += ": " + short(m.cfg.Docker.ContainerName, 22)
	}
	meta := statusUnknown.Padding(0, 1).Render(mode) + "  " + statusUnknown.Padding(0, 1).Render(fmt.Sprintf("%d маршрутов", len(rows))) + "  " + style.Bold(true).Padding(0, 1).Render(label)
	title := accent.Bold(true).Render("HAProxy Bridge") + muted.Render("  /  ") + normal.Bold(true).Render("Маршруты")
	if m.s != nil && string(m.s.Document.Bytes()) != string(m.s.Original) {
		title += warning.Render("  •  Не сохранено")
	}
	headerRows := []string{cell(title, max(1, width-4-ansi.StringWidth(meta))) + meta}
	if m.sidebarWidth() == 0 {
		headerRows = append(headerRows, strings.Join(m.dashboardNavigation(), "   "))
	}
	header := framedRows("", headerRows, width, len(headerRows)+2)
	var list []string
	var search string
	if m.search {
		search = "Поиск: " + m.filter + "▏"
	} else if m.filter != "" {
		search = "Фильтр: " + m.filter + " · Esc сбросить"
	}
	if search != "" {
		list = append(list, accent.Render(search))
	}
	rightTitle := "Детали маршрута"
	var details []string
	if len(rows) > 0 {
		cursor := min(max(0, m.cursor), len(rows)-1)
		_, start, end, detailLimit := m.routeLayout(len(rows), cursor)
		nameWidth := listWidth - 25
		list = append(list, muted.Render("  "+cell("Frontend", nameWidth)+cell("Порт", 6)+"Статус"),
			dashboardBorder.Render(strings.Repeat("─", listWidth-4)))
		for i := start; i < end; i++ {
			route := rows[i]
			status := routeStatus(m.stats.Backends[route.Backend])
			name := "  " + cell(route.Name, nameWidth) + cell(strconv.Itoa(route.Bind.Port), 6)
			badge := statusBadge(short(status, 12))
			padding := ""
			if i == cursor {
				name = dashboardSelection.Render("› " + name[2:])
				padding = dashboardSelection.Render(strings.Repeat(" ", max(0, listWidth-4-ansi.StringWidth(name+badge))))
			}
			list = append(list, name+badge+padding)
		}
		rightTitle += ": " + rows[cursor].Name
		details = m.dashboardDetails(rows[cursor], detailWidth-4)
		offset := min(m.detailOffset, max(0, len(details)-detailLimit))
		details = details[offset:min(len(details), offset+detailLimit)]
	} else if search != "" {
		list = append(list, "Совпадений нет. Esc — сбросить поиск.")
	} else {
		list = append(list, "Маршрутов нет. A — добавить маршрут.")
	}
	columns := lipgloss.JoinHorizontal(lipgloss.Top, framedRows("Маршруты", list, listWidth, height), " ", framedRows(rightTitle, details, detailWidth, height))
	if sidebar := m.sidebarWidth(); sidebar > 0 {
		rows := m.dashboardNavigation()
		if m.cfg.HAProxy.Mode == "docker" {
			rows = append(rows, "", accent.Render("Docker"), m.cfg.Docker.ContainerName, "Apply: "+m.cfg.Docker.ApplyMethod)
		}
		columns = lipgloss.JoinHorizontal(lipgloss.Top, framedRows("Разделы", rows, sidebar, height), " ", columns)
	}
	return header + "\n\n" + columns + "\n\n" + framedRows("", footer, width, len(footer)+2)
}

func (m Model) dashboardDetails(route haproxy.Frontend, width int) []string {
	backend, servers := m.stats.Backends[route.Backend], m.stats.Servers[route.Backend]
	status := routeStatus(backend)
	stateTime := "В состоянии: " + statTime(backend.LastChange)
	if backend.Status == "UP" && backend.LastChange != nil {
		stateTime = "Аптайм: " + statTime(backend.LastChange)
	}
	overview := []string{
		accent.Bold(true).Render(route.Backend) + "  " + statusBadge(status) + muted.Render("  │  ") + stateTime,
		m.routeSessionLine(backend, servers),
	}
	entry := "Вход: " + routeEndpoint(route)
	if backend.Downtime != nil {
		entry += muted.Render("  │  ") + "Простой backend: " + statTime(backend.Downtime)
	}
	panels := []string{framedRows("", overview, width, len(overview)+2), framedRows("", []string{entry}, width, 3)}
	if len(servers) == 0 {
		return strings.Split(strings.Join(panels, "\n"), "\n")
	}
	nameWidth := width - 41
	metricNameWidth := min(16, nameWidth)
	table := []string{muted.Render(cell("Сервер", nameWidth) + cell("Роль", 11) + cell("Статус", 13) + "В состоянии")}
	var metrics []string
	for _, server := range servers {
		role := accent.Render("основной")
		if server.Backup {
			role = muted.Render("резервный")
		}
		state := server.Status
		if state == "" {
			state = "UNKNOWN"
		}
		table = append(table, cell(server.Name, nameWidth)+cell(role, 11)+cell(statusBadge(short(state, 11)), 13)+statTime(server.LastChange))
		prefix := statusStyle(state).UnsetBackground().Render("●") + " " + accent.Bold(true).Render(server.Name)
		metrics = append(metrics, cell(prefix, metricNameWidth)+dashboardBorder.Render(" │ ")+serverSessions(server),
			strings.Repeat(" ", metricNameWidth)+dashboardBorder.Render(" │ ")+"Проверка: "+serverCheck(server))
		if reason := oneLine(server.CheckDetail); reason != "" && !healthyCheck(server.Check) {
			metrics = append(metrics, strings.Repeat(" ", metricNameWidth)+dashboardBorder.Render(" │ ")+warning.Render(reason))
		}
	}
	panels = append(panels, framedRows("Серверы backend", table, width, len(table)+2), framedRows("Метрики серверов", metrics, width, len(metrics)+2))
	gap := "\n"
	if m.height >= 34 {
		gap = "\n\n"
	}
	return strings.Split(strings.Join(panels, gap), "\n")
}

func statTime(value *time.Duration) string {
	if value == nil {
		return "—"
	}
	return value.String()
}

func healthyCheck(check string) bool {
	return strings.HasSuffix(strings.TrimSpace(check), "OK")
}
