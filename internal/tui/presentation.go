package tui

import (
	"fmt"
	"github.com/SawGoD/haproxy-bridge/internal/monitor"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"os"
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
	parts := []string{header, muted.Render(ansi.Truncate(subtitle, w, "…")), muted.Render(strings.Repeat("─", w)), body}
	if m.notice != "" {
		parts = append(parts, warning.Render(ansi.Truncate(strings.ReplaceAll(m.notice, "\n", " "), w, "…")))
	}
	content := strings.Join(parts, "\n")
	lines := strings.Split(content, "\n")
	footer := strings.Split(help, "\n")
	budget := max(3, m.height-len(footer)-3)
	if len(lines) > budget {
		lines = lines[:budget]
	}
	for len(lines) < budget {
		lines = append(lines, "")
	}
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
	case "editor":
		return m.nodeView()
	case "servers":
		return m.serversView()
	case "serverform":
		return m.serverFormView()
	case "confirm":
		return m.screen("Подтверждение", "Проверьте изменения перед продолжением", m.view.View(), hint("Y", "подтвердить")+"    "+hint("N / Esc", "отмена")+"    "+hint("↑↓", "прокрутка"))
	case "view":
		return m.screen("Подробности", "Диагностика и журнал", m.view.View(), hint("Esc", "назад")+"    "+hint("↑↓", "прокрутка"))
	case "missing":
		return m.screen("Файл не найден", m.cfg.General.ConfigPath, "\nКонфигурация HAProxy отсутствует.\n\nУкажите существующий файл в настройках\nили явно создайте новый минимальный конфиг.", hint("O", "настройки")+"    "+hint("C", "создать файл")+"    "+hint("Q", "выход"))
	case "error":
		return m.screen("Не удалось открыть файл", m.cfg.General.ConfigPath, "\nПроверьте путь, права доступа и формат конфигурации.", hint("O", "настройки")+"    "+hint("Q", "выход"))
	case "backups":
		var b strings.Builder
		b.WriteString(muted.Render("  Дата и время           Размер      Проверенная копия") + "\n")
		start := max(0, m.cursor-max(1, m.height-12)+1)
		for i := start; i < len(m.backups) && i < start+max(1, m.height-12); i++ {
			x := m.backups[i]
			line := fmt.Sprintf("  %-21s %-11s %v", x.CreatedAt.Local().Format("02.01.2006 15:04:05"), fmt.Sprintf("%d байт", x.Size), x.Good)
			if i == m.cursor {
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
func (m Model) routesView() string {
	rows := m.rows()
	host, _ := os.Hostname()
	state := fmt.Sprintf("%s  •  %s  •  %d маршрутов", host, m.cfg.HAProxy.Mode, len(rows))
	if m.s != nil && string(m.s.Document.Bytes()) != string(m.s.Original) {
		state += "  •  есть изменения"
	}
	var b strings.Builder
	search := ""
	if m.search || m.filter != "" {
		search = "Поиск: " + m.filter
		if m.search {
			search += "▏"
		}
		b.WriteString(accent.Render(search) + "\n")
	}
	w := max(16, m.width-4)
	nameWidth := max(12, min(24, w-42))
	b.WriteString(muted.Render(fmt.Sprintf("  %-*s %-7s %-16s %s", nameWidth, "Frontend", "Порт", "Активный сервер", "Статус")) + "\n")
	limit := max(2, m.height-16)
	if search != "" {
		limit--
	}
	start := max(0, m.cursor-limit+1)
	end := min(len(rows), start+limit)
	for i := start; i < end; i++ {
		f := rows[i]
		status := "UNKNOWN"
		for _, s := range m.stats {
			if s.Backend == f.Backend {
				if status == "UNKNOWN" {
					status = s.Status
				} else if status != s.Status {
					status = "MIXED"
				}
			}
		}
		line := fmt.Sprintf("  %-*s %-7d %-16s %s", nameWidth, short(f.Name, nameWidth), f.Bind.Port, short(monitor.Active(m.stats, f.Backend), 16), status)
		if i == m.cursor {
			line = selected.Width(w).Render("› " + strings.TrimPrefix(line, "  "))
		} else {
			line = normal.Render(line)
		}
		b.WriteString(line + "\n")
	}
	if len(rows) == 0 {
		b.WriteString("\nМаршрутов нет. Нажмите A, чтобы добавить ноду.\n")
	}
	b.WriteString("\n")
	if len(rows) > 0 {
		f := rows[min(m.cursor, len(rows)-1)]
		b.WriteString(accent.Render(f.Name) + muted.Render("  →  ") + normal.Render(f.Backend) + "\n")
		b.WriteString(muted.Render(fmt.Sprintf("Слушает %s:%d", f.Bind.Address, f.Bind.Port)) + "\n")
		if backend, e := m.s.Document.Backend(f.Backend); e == nil {
			for i, s := range backend.Servers {
				if i >= 2 {
					b.WriteString(muted.Render(fmt.Sprintf("  Ещё серверов: %d • Enter — редактировать", len(backend.Servers)-2)) + "\n")
					break
				}
				role := "основной"
				if s.Backup {
					role = "резервный"
				}
				if s.Disabled {
					role = "выключен"
				}
				b.WriteString(normal.Render(fmt.Sprintf("  %-18s %s:%d", short(s.Name, 18), s.Address, s.Port)) + muted.Render("  "+role) + "\n")
			}
		}
	}
	help := hint("A", "добавить") + "   " + hint("Enter / E", "изменить") + "   " + hint("D", "удалить") + "   " + hint("/", "поиск") + "   " + hint("?", "все действия") + "\n" + hint("S", "сохранить") + "   " + hint("P", "применить") + "   " + hint("B", "копии") + "   " + hint("O", "настройки") + "   " + hint("Q", "выход")
	return m.screen("Маршруты", state, b.String(), help)
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
	title := "Новая нода"
	if m.editing != "" {
		title = "Редактирование ноды"
	}
	return m.screen(title, "Маршрут и параметры соединения", b.String(), hint("Tab / ↑↓", "поле")+"   "+hint("F2", "серверы")+"   "+hint("Ctrl+S", "предпросмотр")+"   "+hint("Esc", "отмена"))
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
	return m.screen("Серверы ноды", "Нода / удалённые серверы", b.String(), hint("A", "добавить")+"   "+hint("Enter", "изменить")+"   "+hint("D", "удалить")+"   "+hint("Ctrl+↑↓", "порядок")+"\n"+hint("Esc", "к ноде"))
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
	return m.screen("Сервер", "Отдельный адрес и параметры проверки", b.String(), hint("Tab / ↑↓", "поле")+"   "+hint("Пробел", "выбор")+"   "+hint("Ctrl+S", "готово")+"   "+hint("Esc", "назад"))
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
	return m.screen(title, "Параметры сохраняются между запусками", b.String(), hint("Ctrl+←→", "раздел")+"   "+hint("Tab / ↑↓", "поле")+"   "+hint("Пробел", "да / нет")+"\n"+hint("Ctrl+S", "сохранить настройки")+"   "+hint("Esc", "назад"))
}
