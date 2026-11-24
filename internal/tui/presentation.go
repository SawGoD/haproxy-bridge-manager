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
	case "editor":
		return m.nodeView()
	case "servers":
		return m.serversView()
	case "serverform":
		return m.serverFormView()
	case "confirm":
		return m.screen("Подтверждение", "Проверьте изменения перед продолжением", m.view.View(), hint("Y", "подтвердить")+"   "+hint("Esc / N", "отмена")+"\n"+hint("↑↓ ←→", "прокрутка"))
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
		actions = []routeAction{{"e", "Редактировать маршрут"}, {"a", "Добавить маршрут"},
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
		state = "Есть несохранённые изменения  •  " + state
	}
	help := hint("Enter", "править") + "   " + hint("Tab / ?", "действия") + "\n" +
		hint("A", "добавить") + "   " + hint("/", "поиск") + "   " + hint("Q", "выход") + "\n" +
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
func (m Model) nodeView() string {
	labels := []string{"Frontend", "Backend", "Адрес прослушивания", "Локальный порт", "Connect timeout", "Client timeout", "Server timeout"}
	var b strings.Builder
	start := max(0, m.focus-max(3, m.height-14)+1)
	end := min(len(m.fields), start+max(3, m.height-14))
	for i := start; i < end; i++ {
		b.WriteString(fieldRow(labels[i], m.fields[i], i == m.focus, m.width-4) + "\n")
	}
	b.WriteString("\n" + accent.Render(fmt.Sprintf("Удалённые серверы  %d", len(m.serverLines()))) + "\n")
	for i, line := range m.serverLines() {
		if i >= 2 {
			break
		}
		v := strings.Fields(line)
		if len(v) == 9 {
			b.WriteString(muted.Render("  "+v[0]+"  ") + normal.Render(v[1]) + "\n")
		}
	}
	title := "Новый маршрут"
	if m.editing != "" {
		title = "Редактирование маршрута"
	}
	return m.screen(title, "Маршрут и параметры соединения", b.String(), hint("Tab / ↑↓", "поле")+"   "+hint("F2", "серверы")+"\n"+hint("Ctrl+S", "предпросмотр")+"   "+hint("Esc", "отмена"))
}
func (m Model) serversView() string {
	var b strings.Builder
	lines := m.serverLines()
	b.WriteString(muted.Render("  Имя                Адрес                     Роль") + "\n")
	start := max(0, m.serverIndex-max(2, m.height-13)+1)
	for i := start; i < len(lines) && i < start+max(2, m.height-13); i++ {
		v := strings.Fields(lines[i])
		if len(v) != 9 {
			continue
		}
		role := "основной"
		if v[2] == "backup" {
			role = "резервный"
		}
		if v[5] == "disabled" {
			role = "выключен"
		}
		row := fmt.Sprintf("  %-18s %-25s %s", short(v[0], 18), short(v[1], 25), role)
		if i == m.serverIndex {
			row = selected.Width(m.width - 4).Render("› " + strings.TrimPrefix(row, "  "))
		}
		b.WriteString(row + "\n")
	}
	if len(lines) == 0 {
		b.WriteString("\nДобавьте первый сервер клавишей A.\n")
	}
	b.WriteString("\n" + muted.Render("Порядок резервов важен при одном основном сервере.") + "\n" + muted.Render("При allbackups применяется балансировка."))
	return m.screen("Серверы ноды", "Нода / удалённые серверы", b.String(), hint("A", "добавить")+"   "+hint("Enter", "изменить")+"   "+hint("D", "удалить")+"\n"+hint("Ctrl+↑↓", "порядок")+"   "+hint("Esc", "к маршруту"))
}
func (m Model) serverFormView() string {
	var b strings.Builder
	start := max(0, m.serverFocus-max(3, m.height-12)+1)
	for i := start; i < len(m.serverFields) && i < start+max(3, m.height-12); i++ {
		f := m.serverFields[i]
		if i == 2 || i == 4 || i == 5 {
			value := displayChoice(f.Value())
			if i == m.serverFocus {
				value = accent.Render("‹ " + value + " ›")
			}
			b.WriteString(fmt.Sprintf("  %-23s %s\n", serverLabels[i], value))
		} else {
			b.WriteString(fieldRow(serverLabels[i], f, i == m.serverFocus, m.width-4) + "\n")
		}
	}
	b.WriteString("\n" + muted.Render("Адрес: host:port или [IPv6]:port. «-» — наследовать."))
	return m.screen("Сервер", "Отдельный адрес и параметры проверки", b.String(), hint("Tab / ↑↓", "поле")+"   "+hint("Пробел", "выбор")+"\n"+hint("Ctrl+S", "готово")+"   "+hint("Esc", "назад"))
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
