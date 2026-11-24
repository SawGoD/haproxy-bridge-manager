package tui

import (
	"fmt"
	"github.com/SawGoD/haproxy-bridge/internal/monitor"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"net"
	"strconv"
	"strings"
)

var normal = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#253447", Dark: "#D8DEE9"})
var muted = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#677789", Dark: "#8B98AA"})
var accent = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#245A9C", Dark: "#83B0F2"})
var selected = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#163D6B", Dark: "#E4EDFA"}).Background(lipgloss.AdaptiveColor{Light: "#E9F0F8", Dark: "#26384E"})
var warning = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#92550D", Dark: "#E0B779"})

func (m Model) screen(title, subtitle, body, help string) string {
	w := max(16, m.width-4)
	header := accent.Bold(true).Render("HAProxy Bridge") + muted.Render("   /   ") + normal.Bold(true).Render(title)
	lines := []string{header, muted.Render(subtitle), muted.Render(strings.Repeat("─", w))}
	footer := strings.Split(help, "\n")
	status := warning.Render(strings.ReplaceAll(m.notice, "\n", " "))
	budget := m.height - len(footer) - 7
	content := strings.Split(strings.TrimSuffix(body, "\n"), "\n")
	lines = append(lines, content[:min(len(content), budget)]...)
	for len(lines) < budget+3 {
		lines = append(lines, "")
	}
	lines = append(lines, status)
	lines = append(lines, muted.Render(strings.Repeat("─", w)))
	lines = append(lines, footer...)
	for i, l := range lines {
		lines[i] = "  " + ansi.Truncate(l, w, "")
	}
	return "\n" + strings.Join(lines, "\n")
}
func hint(k, label string) string { return accent.Render(k) + " " + muted.Render(label) }
func fieldRow(label string, f textinput.Model, focused bool, width int) string {
	f.Prompt = ""
	f.Width = max(10, width-27)
	prefix := "  "
	style := muted
	if focused {
		prefix = "› "
		style = accent
	} else {
		f.Blur()
	}
	return prefix + style.Render(fmt.Sprintf("%-23s", label)) + " " + f.View()
}
func (m Model) View() string {
	if m.width < 48 || m.height < 16 {
		return "HAProxy Bridge\nУвеличьте терминал до 48×16 или больше.\nCtrl+C — выход"
	}
	if m.busy {
		return m.screen("Выполняется операция", "HAProxy • конфигурация и управление", "\n"+accent.Render("  ◌  Проверка и выполнение…"), muted.Render("Дождитесь завершения операции"))
	}
	switch m.mode {
	case "wizard", "settings":
		return m.preferencesView()
	case "actions":
		return m.actionsView()
	case "editor", "servers":
		return m.routeEditorView()
	case "serverform":
		return m.serverFormView()
	case "confirm":
		return m.screen("Подтверждение", "Проверьте изменения перед продолжением", m.view.View(), hint("Y", "подтвердить")+"   "+hint("Q / Esc / N", "отмена")+"\n"+hint("↑↓ ←→", "прокрутка"))
	case "view":
		return m.screen("Подробности", "Диагностика и журнал", m.view.View(), hint("Esc", "назад")+"   "+hint("↑↓ ←→", "прокрутка"))
	case "missing":
		return m.screen("Файл не найден", m.cfg.General.ConfigPath, "\nКонфигурация HAProxy отсутствует.\n\nУкажите существующий файл в настройках\nили явно создайте новый минимальный конфиг.", hint("O", "настройки")+"    "+hint("C", "создать файл")+"    "+hint("Q", "выход"))
	case "error":
		return m.screen("Не удалось открыть файл", m.cfg.General.ConfigPath, "\nПроверьте путь, права доступа и формат конфигурации.", hint("O", "настройки")+"    "+hint("Q", "выход"))
	case "backups":
		var b strings.Builder
		b.WriteString(muted.Render("  Дата и время           Размер      Статус") + "\n")
		start := max(0, m.backupIndex-max(1, m.height-12)+1)
		for i := start; i < len(m.backups) && i < start+max(1, m.height-12); i++ {
			x := m.backups[i]
			state := "обычная"
			if x.Good {
				state = "проверена"
			} else if x.Pinned {
				state = "закреплена"
			}
			line := fmt.Sprintf("  %-21s %-11s %s", x.CreatedAt.Local().Format("02.01.2006 15:04:05"), fmt.Sprintf("%d байт", x.Size), state)
			if i == m.backupIndex {
				line = selected.Render("› " + strings.TrimPrefix(line, "  "))
			}
			b.WriteString(line + "\n")
		}
		if len(m.backups) == 0 {
			b.WriteString("\nРезервных копий пока нет. M на главном экране создаст первую.")
		}
		return m.screen("Резервные копии", m.cfg.General.BackupDir, b.String(), hint("Enter", "diff и восстановление")+"    "+hint("↑↓", "выбор")+"    "+hint("Esc", "назад"))
	}
	return m.routesView()
}

type routeAction struct {
	key, label string
}

func (m Model) routeActions(hasRoute bool) []routeAction {
	actions := []routeAction{{"a", "Добавить маршрут"}}
	if hasRoute {
		actions = []routeAction{{"e", "Редактировать маршрут"}, {"c", "Создать на основании"}, {"a", "Добавить маршрут"},
			{"t", "Проверить TCP"}, {"h", "Проверить TLS"}, {"i", "Статистика Runtime API"}, {"d", "Удалить маршрут"}}
	}
	actions = append(actions, routeAction{"/", "Найти маршрут"},
		routeAction{"v", "Проверить конфигурацию"}, routeAction{"s", "Сохранить в файл"},
		routeAction{"p", "Применить сохранённое"}, routeAction{"x", "Сохранить и применить"},
		routeAction{"b", "Открыть резервные копии"}, routeAction{"m", "Создать резервную копию"},
		routeAction{"r", "Перечитать файл"}, routeAction{"o", "Настройки"}, routeAction{"l", "Журнал действий"})
	if m.cfg.HAProxy.Mode == "docker" {
		actions = append(actions, routeAction{"g", "Логи Docker"})
	}
	return append(actions, routeAction{"q", "Выйти"})
}

func (m Model) actionsView() string {
	rows := m.rows()
	actions := m.routeActions(len(rows) > 0)
	subtitle := "Конфигурация HAProxy"
	if len(rows) > 0 {
		subtitle = rows[min(m.cursor, len(rows)-1)].Name
	}
	var b strings.Builder
	limit := m.height - 9
	start := max(0, m.menuIndex-limit+1)
	for i := start; i < min(len(actions), start+limit); i++ {
		action := actions[i]
		line := fmt.Sprintf("  %-3s %s", strings.ToUpper(action.key), action.label)
		if i == m.menuIndex {
			line = selected.Width(m.width - 4).Render("› " + line[2:])
		}
		b.WriteString(line + "\n")
	}
	footer := hint("↑↓", "выбор") + "   " + hint("Enter", "открыть") + "   " + hint("Esc", "назад")
	return m.screen("Действия", subtitle, b.String(), footer+"\n"+muted.Render(fmt.Sprintf("%d / %d", m.menuIndex+1, len(actions))))
}

func (m Model) routesView() string {
	rows := m.rows()
	state := fmt.Sprintf("%s  •  %d маршрутов", m.cfg.HAProxy.Mode, len(rows))
	if m.s != nil && string(m.s.Document.Bytes()) != string(m.s.Original) {
		state = "Не сохранено  •  " + state
	}
	help := hint("Enter", "править") + "   " + hint("Tab / ?", "действия") + "\n" +
		hint("A", "добавить") + "   " + hint("C", "копия") + "   " + hint("/", "поиск") + "   " + hint("Q", "выход") + "\n" +
		hint("S", "сохранить") + "   " + hint("P", "применить")
	w := m.width - 4
	budget := m.height - 10
	var search string
	if m.search {
		search = accent.Render("Поиск: " + m.filter + "▏")
	} else if m.filter != "" {
		search = accent.Render("Фильтр: " + m.filter + "  •  Esc сбросить")
	}
	if search != "" {
		budget--
		search += "\n"
	}
	if len(rows) == 0 {
		body := "Маршрутов нет. A — добавить маршрут."
		if m.filter != "" {
			body = "Совпадений нет. Esc — сбросить поиск."
		}
		return m.screen("Маршруты", state, search+body, help)
	}
	cursor := min(max(0, m.cursor), len(rows)-1)
	f := rows[cursor]
	bind := net.JoinHostPort(f.Bind.Address, strconv.Itoa(f.Bind.Port))
	active := monitor.Active(m.stats, f.Backend)
	details := []string{accent.Bold(true).Render(f.Name),
		muted.Render("Backend  ") + normal.Render(f.Backend),
		muted.Render("Адрес    ") + normal.Render(bind),
		muted.Render("Активный ") + normal.Render(active)}
	if w < 78 {
		details = []string{accent.Render(f.Name) + muted.Render(" → ") + normal.Render(f.Backend),
			muted.Render("Адрес    ") + normal.Render(bind), muted.Render("Активный ") + normal.Render(active)}
	}
	if backend, err := m.s.Document.Backend(f.Backend); err == nil {
		details = append(details, muted.Render(fmt.Sprintf("Серверы  %d", len(backend.Servers))))
		for _, server := range backend.Servers {
			role := "основной"
			if server.Disabled {
				role = "выключен"
			} else if server.Backup {
				role = "резервный"
			}
			endpoint := net.JoinHostPort(server.Address, strconv.Itoa(server.Port))
			details = append(details, normal.Render(server.Name)+muted.Render("  "+role+"  "+endpoint))
		}
	}
	listWidth, limit := w, max(1, budget-4)
	if budget >= 9 {
		limit = budget - 6
	}
	if w >= 78 {
		listWidth, limit = w/2-1, budget-1
	}
	nameWidth := min(28, listWidth-18)
	header := muted.Render(fmt.Sprintf("  %-*s %-6s %s", nameWidth, "Frontend", "Порт", "Статус"))
	list := []string{header}
	start := max(0, cursor-limit+1)
	for i := start; i < min(len(rows), start+limit); i++ {
		route := rows[i]
		status := "UNKNOWN"
		for _, server := range m.stats {
			if server.Backend == route.Backend {
				if status == "UNKNOWN" {
					status = server.Status
				} else if status != server.Status {
					status = "MIXED"
				}
			}
		}
		line := fmt.Sprintf("  %-*s %-6d %s", nameWidth, short(route.Name, nameWidth), route.Bind.Port, status)
		if i == cursor {
			line = selected.Width(listWidth).Render("› " + line[2:])
		}
		list = append(list, line)
	}
	body := strings.Join(list, "\n")
	if listWidth < w {
		detailWidth := w - listWidth - 3
		for i, line := range details {
			details[i] = ansi.Truncate(line, detailWidth, "…")
		}
		body = lipgloss.JoinHorizontal(lipgloss.Top, lipgloss.NewStyle().Width(listWidth+3).Render(body), strings.Join(details, "\n"))
	} else {
		body += "\n" + strings.Join(details, "\n")
	}
	return m.screen("Маршруты", state, search+body, help)
}
func (m *Model) resizeRouteInputs() {
	width := max(10, m.width-27)
	for _, fields := range [][]textinput.Model{m.fields, m.serverFields} {
		for i := range fields {
			fields[i].Width = width
			fields[i].SetCursor(fields[i].Position())
		}
	}
}

func editorValueRow(label string, field textinput.Model, current, editing bool, width int) string {
	prefix, style := "  ", muted
	if current {
		prefix, style = "› ", accent
	}
	value := field.Value()
	if value == "" {
		value = "—"
	}
	rendered := normal.Render(value)
	if editing {
		rendered = field.View()
	}
	return prefix + style.Render(fmt.Sprintf("%-19s", label)) + "  " + ansi.Truncate(rendered, width-23, "…")
}

func (m Model) routeEditorView() string {
	servers := m.mode == "servers"
	title, subtitle := "Новый маршрут", m.fields[0].Value()
	if m.copySource != "" {
		title, subtitle = "Копия маршрута", m.copySource+" → "+m.fields[0].Value()
	} else if m.editing != "" {
		title = "Маршрут"
	}
	endpoint := m.fields[2].Value()
	if port := m.fields[routePortField].Value(); endpoint != "" && port != "" {
		endpoint = net.JoinHostPort(endpoint, port)
	}
	if subtitle != "" && endpoint != "" {
		subtitle += "  ·  "
	}
	subtitle += endpoint

	serverTab := fmt.Sprintf("Серверы (%d)", len(m.servers))
	tabs := accent.Bold(true).Render("[Маршрут]") + "   " + muted.Render(serverTab)
	if servers {
		tabs = muted.Render("Маршрут") + "   " + accent.Bold(true).Render("["+serverTab+"]")
	}
	rows := []string{tabs}
	width := m.width - 4
	help := hint("↑↓", "выбор") + "   " + hint("Enter", "изменить") + "   " + hint("Tab", "серверы") + "\n" +
		hint("S", "предпросмотр") + "   " + hint("Q / Esc", "назад")
	if m.editing == "" && !servers {
		help += "   " + hint("F3", "порт")
	}
	if m.fieldEditing {
		help = hint("Enter", "готово") + "   " + hint("Esc", "отменить ввод")
	}
	limit := m.height - strings.Count(help, "\n") - 9
	if !servers {
		start := max(0, m.focus-limit+1)
		for i := start; i < min(len(m.fields), start+limit); i++ {
			current := i == m.focus
			rows = append(rows, editorValueRow(routeLabels[i], m.fields[i], current, current && m.fieldEditing, width))
		}
		if m.fieldEditing {
			title = "Ввод · " + routeLabels[m.focus]
		}
		return m.screen(title, subtitle, strings.Join(rows, "\n"), help)
	}

	help = hint("↑↓", "выбор") + "   " + hint("Enter", "открыть") + "   " + hint("Tab", "маршрут") + "\n" +
		hint("A", "добавить") + "   " + hint("D", "удалить") + "   " + hint("Ctrl+↑↓", "порядок") + "\n" +
		hint("S", "предпросмотр") + "   " + hint("Q / Esc", "назад")
	if len(m.servers) == 0 {
		rows = append(rows, "  Серверов пока нет. A — добавить.")
	}
	nameWidth := min(22, width/3-2)
	addressWidth := min(40, width-nameWidth-14)
	start := max(0, m.serverIndex-(m.height-11)+1)
	for i := start; i < min(len(m.servers), start+m.height-11); i++ {
		server := m.servers[i]
		role := "основной"
		if server.Disabled {
			role = "выключен"
		} else if server.Backup {
			role = "резервный"
		}
		address := net.JoinHostPort(server.Address, strconv.Itoa(server.Port))
		if len(address) > addressWidth {
			port := ":" + strconv.Itoa(server.Port)
			address = short(strings.TrimSuffix(address, port), addressWidth-len(port)) + port
		}
		row := fmt.Sprintf("  %-*s %-*s %s", nameWidth, short(server.Name, nameWidth), addressWidth, address, role)
		if i == m.serverIndex {
			row = selected.Render("› " + row[2:])
		}
		rows = append(rows, row)
	}
	return m.screen(title, subtitle, strings.Join(rows, "\n"), help)
}

func (m Model) serverFormView() string {
	title := "Сервер"
	if m.serverNew {
		title = "Новый сервер"
	}
	help := hint("↑↓", "выбор") + "   " + hint("Enter", "изменить") + "\n" +
		hint("S", "в черновик") + "   " + hint("Q / Esc", "назад")
	if m.fieldEditing {
		title = "Ввод · " + serverLabels[m.serverFocus]
		help = hint("Enter", "готово") + "   " + hint("Esc", "отменить ввод")
	}
	limit := m.height - strings.Count(help, "\n") - 8
	start := max(0, m.serverFocus-limit+1)
	var rows []string
	for i := start; i < min(len(m.serverFields), start+limit); i++ {
		field := m.serverFields[i]
		current := i == m.serverFocus
		if i == serverRoleField || i == serverHealthField || i == serverStateField {
			field.SetValue(displayChoice(field.Value()))
		}
		rows = append(rows, editorValueRow(serverLabels[i], field, current, current && m.fieldEditing, m.width-4))
	}
	return m.screen(title, m.fields[0].Value()+" / "+m.serverFields[0].Value(), strings.Join(rows, "\n"), help)
}
func displayChoice(s string) string {
	if v := map[string]string{"primary": "Основной", "backup": "Резервный", "check": "Включён", "nocheck": "Выключен", "inherit": "Наследовать", "enabled": "Включён", "disabled": "Выключен", "true": "Да", "false": "Нет"}[s]; v != "" {
		return v
	}
	return s
}
func (m Model) preferencesView() string {
	if len(m.prefs) == 0 {
		return m.screen("Настройки", "", "Загрузка формы…", hint("Esc", "назад"))
	}
	groups := m.preferenceGroups()
	var b strings.Builder
	b.WriteString(accent.Render(groups[m.prefGroup]) + muted.Render(fmt.Sprintf("  %d / %d", m.prefGroup+1, len(groups))) + "\n\n")
	ids := m.groupFields()
	pos := 0
	for i, id := range ids {
		if id == m.prefIndex {
			pos = i
		}
	}
	start := max(0, pos-max(2, m.height-14)+1)
	for _, id := range ids[start:min(len(ids), start+max(2, m.height-14))] {
		p := m.prefs[id]
		if len(preferenceOptions(p)) > 0 {
			value := displayChoice(p.input.Value())
			if id == m.prefIndex {
				value = accent.Render("‹ " + value + " ›")
			}
			b.WriteString(fmt.Sprintf("  %-23s %s\n", p.label, value))
		} else {
			b.WriteString(fieldRow(p.label, p.input, id == m.prefIndex, m.width-4) + "\n")
		}
	}
	b.WriteString("\n" + muted.Render("Режим: docker / native. Apply: manual / restart / recreate / graceful."))
	title := "Настройки"
	if m.mode == "wizard" {
		title = "Первый запуск"
	}
	return m.screen(title, "Параметры сохраняются между запусками", b.String(), hint("Ctrl+←→", "раздел")+"   "+hint("Tab / ↑↓", "поле")+"\n"+hint("Пробел", "выбор")+"   "+hint("Ctrl+S", "записать")+"   "+hint("Esc", "назад"))
}
