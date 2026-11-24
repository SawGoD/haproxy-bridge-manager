package tui

import (
	"fmt"
	"strings"

	"github.com/SawGoD/haproxy-bridge/internal/backup"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type backupsRefreshed struct {
	list []backup.Metadata
	err  error
}

func (m Model) updateBackups(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "q", "esc":
		m.mode = "main"
	case "j", "down":
		m.backupIndex = min(m.backupIndex+1, max(0, len(m.backups)-1))
	case "k", "up":
		m.backupIndex = max(0, m.backupIndex-1)
	case "enter", "v":
		if len(m.backups) > 0 {
			item := m.backups[m.backupIndex]
			data, err := m.s.Backups.Content(item.ID)
			if err != nil {
				m.notice = err.Error()
			} else {
				m.notice = ""
				m.showDetails("backup-preview", diff(m.s.Original, data))
			}
		}
	case "r":
		m.previewRestore()
	case "c", "m":
		m.busy = true
		return m, func() tea.Msg {
			if _, err := m.s.Backups.Create(m.s.Path); err != nil {
				return backupsRefreshed{err: err}
			}
			list, err := m.s.Backups.List()
			return backupsRefreshed{list, err}
		}
	}
	return m, nil
}

func (m *Model) previewRestore() {
	if len(m.backups) == 0 {
		return
	}
	id := m.backups[m.backupIndex].ID
	data, err := m.s.Backups.Content(id)
	if err != nil {
		m.notice = err.Error()
		return
	}
	m.preview("Восстановление из бэкапа\nЗаменить текущий файл выбранной копией?\nApply выполняется отдельно.\n\n"+diff(m.s.Original, data), func() tea.Msg {
		if err := m.s.Restore(id); err != nil {
			return backupsRefreshed{err: err}
		}
		return result{returnMode: "main", text: "Backup восстановлен. P — применить"}
	})
}

func (m Model) backupsView() string {
	help := hint("↑↓", "выбор") + "   " + hint("Enter", "просмотр") + "   " + hint("Q / Esc", "назад") + "\n" +
		hint("R", "восстановить") + "   " + hint("C", "создать копию")
	subtitle := fmt.Sprintf("%d / %d  %s", len(m.backups), m.cfg.General.MaxBackups, m.cfg.General.BackupDir)
	if len(m.backups) == 0 {
		return m.screen("Резервные копии", subtitle, "Резервных копий пока нет. C — создать.", help)
	}
	wide := m.width >= 86
	limit := m.height - 10
	if wide {
		limit -= 2
	}
	start := max(0, m.backupIndex-limit+1)
	rows := []string{muted.Render("  " + cell("Дата и время", 20) + cell("Размер", 10) + "SHA-256")}
	for i := start; i < min(len(m.backups), start+limit); i++ {
		item := m.backups[i]
		row := "  " + cell(item.CreatedAt.Local().Format("02.01.2006 15:04:05"), 20) + cell(fmt.Sprintf("%d Б", item.Size), 10) + short(item.SHA256, 10)
		if i == m.backupIndex {
			row = dashboardSelection.Render("› " + row[2:])
		}
		rows = append(rows, row)
	}
	if !wide {
		return m.screen("Резервные копии", subtitle, strings.Join(rows, "\n"), help)
	}
	item := m.backups[m.backupIndex]
	info := []string{accent.Render("Файл"), item.ID, "", fmt.Sprintf("Размер: %d байт", item.Size),
		"Создан: " + item.CreatedAt.Local().Format("02.01.2006 15:04:05"), "", accent.Render("SHA-256"), item.SHA256[:min(24, len(item.SHA256))], item.SHA256[min(24, len(item.SHA256)):], ""}
	if item.Good {
		info = append(info, statusUp.Render("Проверенная точка"))
	}
	if item.Pinned {
		info = append(info, "Закреплена")
	}
	width := m.width - 4
	listWidth, height := width*53/100, m.height-9
	body := lipgloss.JoinHorizontal(lipgloss.Top, framedRows("Копии", rows, listWidth, height), " ",
		framedRows("Информация", info, width-listWidth-1, height))
	return m.screen("Резервные копии", subtitle, body, help)
}
