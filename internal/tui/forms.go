package tui

import (
	"fmt"
	"github.com/SawGoD/haproxy-bridge/internal/haproxy"
	"net"
	"reflect"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

type preference struct {
	section, label string
	path           []int
	kind           reflect.Kind
	input          textinput.Model
}

var sectionNames = map[string]string{"General": "Файлы", "HAProxy": "HAProxy", "Docker": "Docker", "Native": "Native", "Network": "Сеть", "Healthcheck": "Проверки", "Monitoring": "Мониторинг", "UI": "Интерфейс"}
var fieldNames = map[string]string{"ConfigPath": "Конфигурация HAProxy", "BackupDir": "Каталог резервных копий", "AutoBackup": "Автоматический backup", "MaxBackups": "Количество копий", "Mode": "Способ запуска", "ValidationEnabled": "Проверка конфигурации", "ValidationTimeout": "Ожидание проверки, сек", "ContainerName": "Имя контейнера", "ComposeFile": "Файл Compose", "ServiceName": "Имя сервиса", "ApplyMethod": "Способ применения", "Binary": "Бинарник HAProxy", "DefaultBindAddress": "Адрес прослушивания", "DefaultStartPort": "Первый локальный порт", "DefaultTargetPort": "Порт удалённого сервера", "ConnectTimeout": "Ожидание TCP, сек", "SNI": "TLS SNI", "Enabled": "Health checks", "Interval": "Интервал, сек", "Fall": "Ошибок до DOWN", "Rise": "Успехов до UP", "Socket": "Unix socket Runtime API", "RefreshInterval": "Обновление, сек", "Confirm": "Подтверждение Apply", "Advanced": "Расширенные параметры"}

func New(p string) Model {
	m := newLegacy(p)
	originalNotice := m.notice
	if m.mode == "wizard" || m.mode == "settings" {
		first := m.mode == "wizard"
		m.beginPreferences(first)
		if !first {
			m.notice = originalNotice
		}
	}
	return m
}
func input(value string) textinput.Model {
	f := textinput.New()
	f.Prompt = ""
	f.CharLimit = 4096
	f.SetValue(value)
	f.Width = 45
	f.TextStyle = normal
	f.Cursor.Style = accent
	return f
}
func (m *Model) beginPreferences(first bool) {
	m.prefs = nil
	m.prefIndex = 0
	m.prefGroup = 0
	v := reflect.ValueOf(m.cfg)
	typ := v.Type()
	for i := 0; i < v.NumField(); i++ {
		if v.Field(i).Kind() != reflect.Struct {
			continue
		}
		section := sectionNames[typ.Field(i).Name]
		for j := 0; j < v.Field(i).NumField(); j++ {
			f := v.Field(i).Field(j)
			name := v.Field(i).Type().Field(j).Name
			m.prefs = append(m.prefs, preference{section: section, label: fieldNames[name], path: []int{i, j}, kind: f.Kind(), input: input(fmt.Sprint(f.Interface()))})
		}
	}
	if first {
		m.mode = "wizard"
	} else {
		m.mode = "settings"
	}
	m.notice = ""
	if first {
		m.notice = "Укажите тестовый конфиг и способ запуска HAProxy."
	}
	if len(m.prefs) > 0 {
		m.prefs[0].input.Focus()
	}
}
func (m Model) preferenceGroups() []string {
	var out []string
	for _, p := range m.prefs {
		if len(out) == 0 || out[len(out)-1] != p.section {
			out = append(out, p.section)
		}
	}
	return out
}
func (m Model) groupFields() []int {
	g := m.preferenceGroups()
	if len(g) == 0 {
		return nil
	}
	var ids []int
	for i, p := range m.prefs {
		if p.section == g[m.prefGroup] {
			ids = append(ids, i)
		}
	}
	return ids
}
func (m *Model) movePref(delta int) {
	ids := m.groupFields()
	if len(ids) == 0 {
		return
	}
	pos := 0
	for i, id := range ids {
		if id == m.prefIndex {
			pos = i
		}
	}
	m.prefs[m.prefIndex].input.Blur()
	m.prefIndex = ids[(pos+delta+len(ids))%len(ids)]
	m.prefs[m.prefIndex].input.Focus()
}
func (m *Model) savePreferences() error {
	next := m.cfg
	v := reflect.ValueOf(&next).Elem()
	for _, p := range m.prefs {
		dst := v.Field(p.path[0]).Field(p.path[1])
		value := strings.TrimSpace(p.input.Value())
		switch p.kind {
		case reflect.String:
			dst.SetString(value)
		case reflect.Bool:
			b, e := strconv.ParseBool(value)
			if e != nil {
				return fmt.Errorf("%s: выберите да или нет", p.label)
			}
			dst.SetBool(b)
		case reflect.Int:
			n, e := strconv.Atoi(value)
			if e != nil {
				return fmt.Errorf("%s: введите число", p.label)
			}
			dst.SetInt(int64(n))
		}
	}
	if e := next.Validate(); e != nil {
		return e
	}
	if m.s != nil && string(m.s.Document.Bytes()) != string(m.s.Original) {
		m.preview("Настройки изменены. Перечитать файл и отменить несохранённые изменения маршрутов?\n"+diff(m.s.Original, m.s.Document.Bytes()), func() tea.Msg {
			e := next.Save(m.settingsPath)
			return result{err: e, open: true, settingsData: &next}
		})
		return nil
	}
	if e := next.Save(m.settingsPath); e != nil {
		return e
	}
	m.cfg = next
	m.open()
	return nil
}
func (m Model) serverLines() []string {
	var out []string
	for _, l := range strings.Split(m.area.Value(), "\n") {
		if strings.TrimSpace(l) != "" {
			out = append(out, l)
		}
	}
	return out
}
func (m *Model) openServer(add bool) {
	lines := m.serverLines()
	values := []string{"", "", "primary", "1", "check", "enabled", "3s", "2", "2"}
	if !add {
		if len(lines) == 0 {
			return
		}
		m.serverIndex = min(m.serverIndex, len(lines)-1)
		values = strings.Fields(lines[m.serverIndex])
		if len(values) != 9 {
			m.notice = "Не удалось прочитать параметры сервера"
			return
		}
	} else {
		values[0] = fmt.Sprintf("server%d", len(lines)+1)
		values[1] = fmt.Sprintf("example.com:%d", m.cfg.Network.DefaultTargetPort)
		values[6] = fmt.Sprintf("%ds", m.cfg.Healthcheck.Interval)
		values[7] = strconv.Itoa(m.cfg.Healthcheck.Fall)
		values[8] = strconv.Itoa(m.cfg.Healthcheck.Rise)
		if !m.cfg.Healthcheck.Enabled {
			values[4] = "nocheck"
		}
	}
	m.serverFields = nil
	for _, s := range values {
		m.serverFields = append(m.serverFields, input(s))
	}
	m.serverFocus = 0
	m.serverFields[0].Focus()
	m.serverNew = add
	m.mode = "serverform"
	m.notice = ""
}
func (m *Model) storeServer() error {
	v := make([]string, len(m.serverFields))
	for i, f := range m.serverFields {
		v[i] = strings.TrimSpace(f.Value())
		if v[i] == "" || strings.ContainsAny(v[i], " \t\n\r") {
			return fmt.Errorf("Заполните поле «%s»", serverLabels[i])
		}
	}
	if e := validateServerFields(v); e != nil {
		return e
	}
	lines := m.serverLines()
	for i, l := range lines {
		if !m.serverNew && i == m.serverIndex {
			continue
		}
		if strings.Fields(l)[0] == v[0] {
			return fmt.Errorf("Сервер с таким именем уже существует")
		}
	}
	line := strings.Join(v, " ")
	if m.serverNew {
		lines = append(lines, line)
		m.serverIndex = len(lines) - 1
	} else {
		lines[m.serverIndex] = line
	}
	m.area.SetValue(strings.Join(lines, "\n"))
	m.mode = "servers"
	m.notice = ""
	return nil
}

var serverLabels = []string{"Имя сервера", "Адрес и порт", "Роль", "Вес", "Health check", "Состояние", "Интервал", "Fall", "Rise"}

func cycle(value string, values ...string) string {
	for i, v := range values {
		if value == v {
			return values[(i+1)%len(values)]
		}
	}
	return values[0]
}
func (m Model) formUpdate(msg tea.Msg) (Model, tea.Cmd, bool) {
	key, ok := msg.(tea.KeyMsg)
	if !ok || m.busy || key.String() == "ctrl+c" {
		return m, nil, false
	}
	k := key.String()
	if m.mode == "wizard" || m.mode == "settings" {
		if len(m.prefs) == 0 {
			originalNotice := m.notice
			first := m.mode == "wizard"
			m.beginPreferences(first)
			if !first {
				m.notice = originalNotice
			}
		}
		switch k {
		case "esc":
			if m.s == nil {
				return m, tea.Quit, true
			}
			m.mode = "main"
			return m, nil, true
		case "ctrl+s":
			if e := m.savePreferences(); e != nil {
				m.notice = e.Error()
			}
			return m, nil, true
		case "tab", "down":
			m.movePref(1)
			return m, nil, true
		case "shift+tab", "up":
			m.movePref(-1)
			return m, nil, true
		case "ctrl+left", "ctrl+right":
			delta := 1
			if k == "ctrl+left" {
				delta = -1
			}
			m.prefs[m.prefIndex].input.Blur()
			m.prefGroup = (m.prefGroup + delta + len(m.preferenceGroups())) % len(m.preferenceGroups())
			m.prefIndex = m.groupFields()[0]
			m.prefs[m.prefIndex].input.Focus()
			return m, nil, true
		case " ", "enter":
			p := &m.prefs[m.prefIndex]
			options := preferenceOptions(*p)
			if len(options) > 0 {
				p.input.SetValue(cycle(p.input.Value(), options...))
				return m, nil, true
			}
		}
		var cmd tea.Cmd
		m.prefs[m.prefIndex].input, cmd = m.prefs[m.prefIndex].input.Update(msg)
		return m, cmd, true
	}
	if m.mode == "editor" {
		switch k {
		case "esc":
			m.mode = "main"
			return m, nil, true
		case "ctrl+s":
			if e := m.candidate(); e != nil {
				m.notice = e.Error()
			}
			return m, nil, true
		case "f2":
			m.mode = "servers"
			m.serverIndex = 0
			m.notice = ""
			return m, nil, true
		case "tab", "shift+tab", "down", "up":
			delta := 1
			if k == "shift+tab" || k == "up" {
				delta = -1
			}
			m.fields[m.focus].Blur()
			m.focus = (m.focus + delta + len(m.fields)) % len(m.fields)
			m.fields[m.focus].Focus()
			return m, nil, true
		}
		var cmd tea.Cmd
		m.fields[m.focus], cmd = m.fields[m.focus].Update(msg)
		return m, cmd, true
	}
	if m.mode == "servers" {
		lines := m.serverLines()
		switch k {
		case "esc":
			m.mode = "editor"
		case "a":
			m.openServer(true)
		case "e", "enter":
			m.openServer(false)
		case "j", "down":
			m.serverIndex = min(m.serverIndex+1, max(0, len(lines)-1))
		case "k", "up":
			m.serverIndex = max(0, m.serverIndex-1)
		case "ctrl+up", "ctrl+down":
			other := m.serverIndex + 1
			if k == "ctrl+up" {
				other = m.serverIndex - 1
			}
			if other >= 0 && other < len(lines) {
				lines[other], lines[m.serverIndex] = lines[m.serverIndex], lines[other]
				m.serverIndex = other
				m.area.SetValue(strings.Join(lines, "\n"))
			}
		case "d":
			if len(lines) > 0 {
				i := m.serverIndex
				name := strings.Fields(lines[i])[0]
				m.preview("Удалить сервер "+name+" из редактируемой ноды?\nИзменение будет записано только после Save.", func() tea.Msg {
					remaining := append(append([]string{}, lines[:i]...), lines[i+1:]...)
					data := strings.Join(remaining, "\n")
					return result{serverData: &data, returnMode: "servers", text: "Сервер удалён из кандидата"}
				})
			}
		}
		return m, nil, true
	}
	if m.mode == "serverform" {
		switch k {
		case "esc":
			m.mode = "servers"
			return m, nil, true
		case "ctrl+s":
			if e := m.storeServer(); e != nil {
				m.notice = e.Error()
			}
			return m, nil, true
		case "tab", "shift+tab", "down", "up":
			delta := 1
			if k == "shift+tab" || k == "up" {
				delta = -1
			}
			m.serverFields[m.serverFocus].Blur()
			m.serverFocus = (m.serverFocus + delta + len(m.serverFields)) % len(m.serverFields)
			m.serverFields[m.serverFocus].Focus()
			return m, nil, true
		case " ", "enter":
			f := &m.serverFields[m.serverFocus]
			switch m.serverFocus {
			case 2:
				f.SetValue(cycle(f.Value(), "primary", "backup"))
				return m, nil, true
			case 4:
				f.SetValue(cycle(f.Value(), "inherit", "check", "nocheck"))
				return m, nil, true
			case 5:
				f.SetValue(cycle(f.Value(), "enabled", "disabled"))
				return m, nil, true
			}
		}
		var cmd tea.Cmd
		m.serverFields[m.serverFocus], cmd = m.serverFields[m.serverFocus].Update(msg)
		return m, cmd, true
	}
	return m, nil, false
}

func validateServerFields(v []string) error {
	host, p, e := net.SplitHostPort(v[1])
	if e != nil {
		return fmt.Errorf("Адрес: используйте host:port или [IPv6]:port")
	}
	port, e := strconv.Atoi(p)
	if e != nil {
		return e
	}
	weight, e := optionalNumber(v[3])
	if e != nil {
		return fmt.Errorf("Вес: введите 0–256 или -")
	}
	fall, e := optionalNumber(v[7])
	if e != nil {
		return e
	}
	rise, e := optionalNumber(v[8])
	if e != nil {
		return e
	}
	if v[7] != "-" && fall < 1 || v[8] != "-" && rise < 1 {
		return fmt.Errorf("Fall и Rise должны быть положительными")
	}
	if v[2] != "primary" && v[2] != "backup" || v[4] != "check" && v[4] != "nocheck" && v[4] != "inherit" || v[5] != "enabled" && v[5] != "disabled" {
		return fmt.Errorf("Выберите роль, health check и состояние пробелом")
	}
	interval := v[6]
	if interval == "-" {
		interval = ""
	}
	d, _ := haproxy.Parse(nil)
	return d.CreateBackend(haproxy.Backend{Name: "validation", Servers: []haproxy.Server{{Name: v[0], Address: host, Port: port, Weight: weight, WeightSet: v[3] != "-", Fall: fall, Rise: rise, Interval: interval}}})
}

func preferenceOptions(p preference) []string {
	if p.kind == reflect.Bool {
		return []string{"true", "false"}
	}
	if p.label == "Способ запуска" {
		return []string{"docker", "native"}
	}
	if p.label == "Способ применения" {
		return []string{"manual", "restart", "recreate", "graceful"}
	}
	return nil
}
