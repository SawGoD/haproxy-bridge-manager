package tui

import (
	"fmt"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"net"
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
	if m.width >= 86 {
		return framedRows("", lines, m.width, m.height)
	}
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
	if m.mode == "operation" {
		return m.operationView()
	}
	if m.busy {
		return m.screen("Выполняется операция", "HAProxy • конфигурация и управление", "\n"+accent.Render("  ◌  Проверка и выполнение…"), muted.Render("Дождитесь завершения операции"))
	}
	switch m.mode {
	case "wizard", "settings":
		return m.preferencesView()
	case "actions":
		return m.actionsView()
	case "overview":
		return m.overviewView()
	case "editor", "servers":
		return m.routeEditorView()
	case "serverform":
		return m.serverFormView()
	case "confirm":
		return m.screen("Подтверждение", "Проверьте изменения перед продолжением", m.view.View(), hint("Y", "подтвердить")+"   "+hint("Q / Esc / N", "отмена")+"\n"+hint("↑↓ ←→", "прокрутка"))
	case "view":
		title := m.viewTitle
		if title == "" {
			title = "Подробности"
		}
		return m.screen(title, "", m.view.View(), hint("Q / Esc", "назад")+"   "+hint("↑↓ ←→", "прокрутка"))
	case "probe":
		return m.screen("Проверка маршрута", m.probe.route+" · "+m.probe.kind, m.view.View(),
			hint("R", "повторить")+"   "+hint("Q / Esc", "назад")+"\n"+hint("↑↓ ←→", "прокрутка"))
	case "backup-preview":
		item := m.backups[m.backupIndex]
		return m.screen("Просмотр бэкапа", item.ID, m.view.View(),
			hint("R", "восстановить")+"   "+hint("Q / Esc", "назад")+"\n"+hint("↑↓ ←→", "прокрутка"))
	case "apply-help":
		return m.screen("Применение", "Docker", m.view.View(), hint("O", "способ применения")+"   "+hint("Esc", "назад"))
	case "missing":
		return m.screen("Файл не найден", m.cfg.General.ConfigPath, "\nКонфигурация HAProxy отсутствует.\n\nУкажите существующий файл в настройках\nили явно создайте новый минимальный конфиг.", hint("O", "настройки")+"    "+hint("C", "создать файл")+"    "+hint("Q", "выход"))
	case "error":
		return m.screen("Не удалось открыть файл", m.cfg.General.ConfigPath, "\nПроверьте путь, права доступа и формат конфигурации.", hint("O", "настройки")+"    "+hint("Q", "выход"))
	case "backups":
		return m.backupsView()
	}
	return m.routesView()
}

type routeAction struct {
	key, label string
}

func (m Model) routeActions(hasRoute bool) []routeAction {
	actions := []routeAction{{"a", "Добавить маршрут"}}
	if hasRoute {
		actions = []routeAction{{"c", "Создать на основании"}, {"a", "Добавить маршрут"},
			{"t", "Проверка конфига и тест TCP"}, {"h", "Проверка конфига и тест TLS"}, {"d", "Удалить маршрут"}}
	} else {
		actions = append(actions, routeAction{"t", "Проверить конфигурацию"})
	}
	actions = append(actions, routeAction{"/", "Найти маршрут"},
		routeAction{"s", "Сохранить в файл"},
		routeAction{"p", "Применить сохранённое"}, routeAction{"x", "Сохранить и применить"},
		routeAction{"b", "Открыть резервные копии"}, routeAction{"m", "Создать резервную копию"},
		routeAction{"r", "Перечитать файл"}, routeAction{"o", "Настройки"}, routeAction{"l", "Журнал действий"})
	if m.cfg.HAProxy.Mode == "docker" {
		actions = append(actions, routeAction{"g", "Контейнер Docker и логи"})
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
	if m.useDashboard() {
		return m.dashboardView(rows)
	}
	state := fmt.Sprintf("%s  •  %d маршрутов", m.cfg.HAProxy.Mode, len(rows))
	if m.s != nil && string(m.s.Document.Bytes()) != string(m.s.Original) {
		state = "Не сохранено  •  " + state
	}
	help := hint("↑↓", "маршрут") + "   " + hint("Enter", "обзор") + "   " + hint("Pg↑↓", "детали") + "\n" +
		hint("A", "добавить") + "   " + hint("C", "копия") + "   " + hint("D", "удалить") + "   " + hint("T", "тест") + "\n" +
		hint("S", "сохранить") + "  " + hint("P", "применить") + "  " + hint("Q", "выход") + "  " + hint("/", "поиск")
	w := m.width - 4
	sections := m.dashboardNavigation()
	navigation := strings.Join(sections[1:3], "  ") + "  " + hint("Tab", "меню") + "\n" +
		strings.Join(sections[3:], "   ") + "\n"
	var search string
	if m.search {
		search = accent.Render("Поиск: " + m.filter + "▏")
	} else if m.filter != "" {
		search = accent.Render("Фильтр: " + m.filter + "  •  Esc сбросить")
	}
	if search != "" {
		search += "\n"
	}
	if len(rows) == 0 {
		body := "Маршрутов нет. A — добавить маршрут."
		if m.filter != "" {
			body = "Совпадений нет. Esc — сбросить поиск."
		}
		return m.screen("Маршруты", state, navigation+search+body, help)
	}
	cursor := min(max(0, m.cursor), len(rows)-1)
	details := m.routeDetails(rows[cursor])
	listWidth, start, end, detailLimit := m.routeLayout(len(rows), cursor)
	nameWidth := min(24, listWidth-23)
	header := muted.Render(fmt.Sprintf("  %-*s %-6s %s", nameWidth, "Frontend", "Порт", "Статус"))
	list := []string{header}
	for i := start; i < end; i++ {
		route := rows[i]
		status := routeStatus(m.stats.Backends[route.Backend])
		line := fmt.Sprintf("  %-*s %-6d ", nameWidth, short(route.Name, nameWidth), route.Bind.Port)
		if i == cursor {
			line = dashboardSelection.Render("› " + line[2:])
		}
		list = append(list, line+statusStyle(status).Bold(true).Render(short(status, listWidth-nameWidth-10)))
	}
	body := strings.Join(list, "\n")
	offset := min(m.detailOffset, max(0, len(details)-detailLimit))
	details = details[offset:min(len(details), offset+detailLimit)]
	if listWidth < w {
		detailWidth := w - listWidth - 3
		for i, line := range details {
			details[i] = ansi.Truncate(line, detailWidth, "…")
		}
		body = lipgloss.JoinHorizontal(lipgloss.Top, lipgloss.NewStyle().Width(listWidth+3).Render(body), strings.Join(details, "\n"))
	} else {
		body += "\n" + strings.Join(details, "\n")
	}
	return m.screen("Маршруты", state, navigation+search+body, help)
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
		title = "Редактор маршрута"
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
		hint("A", "добавить") + "   " + hint("D", "удалить") + "   " + hint("[ ]", "порядок") + "\n" +
		hint("Space", "вкл/выкл") + "   " + hint("S", "diff") + "   " + hint("Q / Esc", "назад")
	table := m.serverTable(m.servers, m.fields[1].Value(), m.serverIndex, width)
	start := max(1, m.serverIndex-(m.height-12)+2)
	if len(table) > 0 {
		rows = append(rows, table[0])
		rows = append(rows, table[min(start, len(table)):min(len(table), start+m.height-12)]...)
	}
	return m.screen(title, subtitle, strings.Join(rows, "\n"), help)
}

func (m Model) serverFormView() string {
	title := "Сервер"
	if m.serverNew {
		title = "Новый сервер"
	}
	help := hint("↑↓", "поле") + "   " + hint("Enter", "изменить") + "   " + hint("Tab", "разделы") + "\n" +
		hint("S", "в черновик") + "   " + hint("Q / Esc", "назад")
	if m.fieldEditing {
		title = "Ввод · " + serverLabels[m.serverFocus]
		help = hint("Enter", "готово") + "   " + hint("Esc", "отменить ввод")
	}
	group := serverSection(m.serverFocus)
	wide := m.width >= 86
	width := m.overviewWidth()
	sections := sectionRows(serverSectionNames, group, wide)
	var rows []string
	for _, i := range serverFieldGroups[group] {
		field := m.serverFields[i]
		current := i == m.serverFocus
		if i == serverRoleField || i == serverHealthField || i == serverStateField {
			field.SetValue(displayChoice(field.Value()))
		}
		rows = append(rows, editorValueRow(serverLabels[i], field, current, current && m.fieldEditing, width))
	}
	var body string
	if wide {
		height := m.height - strings.Count(help, "\n") - 8
		body = lipgloss.JoinHorizontal(lipgloss.Top, framedRows("Разделы", sections, 20, height), " ",
			framedRows(serverSectionNames[group], rows, width+4, height))
	} else {
		body = strings.Join(append(sections, rows...), "\n")
	}
	return m.screen(title, m.fields[0].Value()+" / "+m.serverFields[0].Value(), body, help)
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
			prefix := "  "
			if id == m.prefIndex {
				prefix = "› "
				value = accent.Render("‹ " + value + " ›")
			}
			b.WriteString(fmt.Sprintf("%s%-23s %s\n", prefix, p.label, value))
		} else {
			b.WriteString(fieldRow(p.label, p.input, id == m.prefIndex, m.width-4) + "\n")
		}
	}
	b.WriteString("\n" + muted.Render("Режим: docker / native. Apply: manual / restart / recreate / graceful."))
	title := "Настройки"
	if m.mode == "wizard" {
		title = "Первый запуск"
	}
	return m.screen(title, "Параметры сохраняются между запусками", b.String(), hint("Tab / Shift+Tab", "раздел")+"   "+hint("↑↓", "поле")+"\n"+hint("Пробел", "выбор")+"   "+hint("Ctrl+S", "записать")+"   "+hint("Esc", "назад"))
}
