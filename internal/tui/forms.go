package tui

import (
	"fmt"
	"github.com/SawGoD/haproxy-bridge/internal/haproxy"
	"reflect"
	"slices"
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
	m.selectPreference(ids[(pos+delta+len(ids))%len(ids)])
}

func (m *Model) selectPreference(index int) {
	m.prefs[m.prefIndex].input.Blur()
	m.prefIndex = index
	m.prefs[index].input.Focus()
}

func (m *Model) openDockerApplyPreferences() {
	m.beginPreferences(false)
	for index, group := range m.preferenceGroups() {
		if group != sectionNames["Docker"] {
			continue
		}
		m.prefGroup = index
		for _, field := range m.groupFields() {
			if m.prefs[field].label == fieldNames["ApplyMethod"] {
				m.selectPreference(field)
				return
			}
		}
	}
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
	if m.s != nil && next.Docker.ApplyMethod != m.cfg.Docker.ApplyMethod {
		methodOnly := m.cfg
		methodOnly.Docker.ApplyMethod = next.Docker.ApplyMethod
		if methodOnly == next {
			if err := next.Save(m.settingsPath); err != nil {
				return err
			}
			m.cfg, m.s.Settings = next, next
			m.mode, m.notice = "main", ""
			return nil
		}
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
func (m *Model) openServer(add bool) {
	var server haproxy.Server
	if !add {
		if len(m.servers) == 0 {
			return
		}
		server = m.servers[m.serverIndex]
	} else {
		for i := 1; ; i++ {
			server.Name = "server" + strconv.Itoa(i)
			if !slices.ContainsFunc(m.servers, func(s haproxy.Server) bool { return s.Name == server.Name }) {
				break
			}
		}
		server.Weight, server.WeightSet = 1, true
		server.Port = m.cfg.Network.DefaultTargetPort
		server.HealthCheck = m.cfg.Healthcheck.Enabled
		server.CheckDisabled = !server.HealthCheck
		server.Interval = fmt.Sprintf("%ds", m.cfg.Healthcheck.Interval)
		server.Fall, server.Rise = m.cfg.Healthcheck.Fall, m.cfg.Healthcheck.Rise
	}
	role, state, health := "primary", "enabled", "inherit"
	if server.Backup {
		role = "backup"
	}
	if server.Disabled {
		state = "disabled"
	}
	if server.HealthCheck {
		health = "check"
	} else if server.CheckDisabled {
		health = "nocheck"
	}
	weight, interval, fall, rise := "-", "-", "-", "-"
	if server.WeightSet || server.Weight != 0 {
		weight = strconv.Itoa(server.Weight)
	}
	if server.Interval != "" {
		interval = server.Interval
	}
	if server.Fall != 0 {
		fall = strconv.Itoa(server.Fall)
	}
	if server.Rise != 0 {
		rise = strconv.Itoa(server.Rise)
	}
	m.serverFields = nil
	for _, value := range []string{server.Name, server.Address, strconv.Itoa(server.Port), role, weight, health, state, interval, fall, rise} {
		m.serverFields = append(m.serverFields, input(value))
	}
	m.serverFields[1].Placeholder = "IP или DNS"
	m.serverFocus = 1
	m.fieldEditing = false
	m.serverNew = add
	m.mode = "serverform"
	m.notice = ""
	m.resizeRouteInputs()
}

func (m *Model) storeServer() error {
	values := make([]string, len(m.serverFields))
	for i, field := range m.serverFields {
		values[i] = strings.TrimSpace(field.Value())
		if values[i] == "" || strings.ContainsAny(values[i], " \t\n\r") {
			m.serverFocus = i
			return fmt.Errorf("Заполните поле «%s»", serverLabels[i])
		}
	}
	server, err := parseServerFields(values)
	if err != nil {
		return err
	}
	for i, existing := range m.servers {
		if existing.Name == server.Name && (m.serverNew || i != m.serverIndex) {
			m.serverFocus = 0
			return fmt.Errorf("Сервер с таким именем уже существует")
		}
	}
	if m.serverNew {
		m.servers = append(m.servers, server)
		m.serverIndex = len(m.servers) - 1
	} else {
		server.Role = m.servers[m.serverIndex].Role
		m.servers[m.serverIndex] = server
	}
	m.mode = "servers"
	m.notice = ""
	return nil
}

func (m *Model) startFieldEdit(field *textinput.Model) tea.Cmd {
	m.fieldBefore = field.Value()
	m.fieldEditing = true
	field.CursorEnd()
	return field.Focus()
}

func (m *Model) updateFieldEdit(field *textinput.Model, key tea.KeyMsg) tea.Cmd {
	switch key.String() {
	case "esc":
		field.SetValue(m.fieldBefore)
		fallthrough
	case "enter":
		field.Blur()
		m.fieldEditing = false
		return nil
	default:
		var cmd tea.Cmd
		*field, cmd = field.Update(key)
		return cmd
	}
}

const (
	serverRoleField   = 3
	serverHealthField = 5
	serverStateField  = 6
)

var serverLabels = []string{"Имя сервера", "Адрес", "Порт", "Роль", "Вес", "Health check", "Состояние", "Интервал", "Fall", "Rise"}
var serverSectionNames = []string{"Общее", "Health check", "Дополнительно"}
var serverFieldGroups = [][]int{{0, 1, 2, serverRoleField, serverStateField}, {serverHealthField, 7, 8, 9}, {4}}

func serverSection(field int) int {
	for i, fields := range serverFieldGroups {
		if slices.Contains(fields, field) {
			return i
		}
	}
	return 0
}

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
		case "down":
			m.movePref(1)
			return m, nil, true
		case "up":
			m.movePref(-1)
			return m, nil, true
		case "tab", "shift+tab", "ctrl+left", "ctrl+right":
			delta := 1
			if k == "shift+tab" || k == "ctrl+left" {
				delta = -1
			}
			groups := len(m.preferenceGroups())
			m.prefGroup = (m.prefGroup + delta + groups) % groups
			m.selectPreference(m.groupFields()[0])
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
	if m.mode == "editor" || m.mode == "servers" {
		if m.fieldEditing {
			cmd := m.updateFieldEdit(&m.fields[m.focus], key)
			return m, cmd, true
		}
		switch k {
		case "s", "ctrl+s":
			if err := m.candidate(); err != nil {
				m.notice = err.Error()
			}
			return m, nil, true
		case "f3":
			if m.editing == "" {
				if _, err := m.draftPort(true); err != nil {
					m.notice = err.Error()
				}
				return m, nil, true
			}
		}
	}
	if m.mode == "editor" {
		switch k {
		case "q", "esc":
			m.mode = m.editBack
			if m.mode == "" {
				m.mode = "main"
			}
			m.notice = ""
			return m, nil, true
		case "f2", "tab", "shift+tab", "right":
			m.mode = "servers"
			return m, nil, true
		case "enter":
			cmd := m.startFieldEdit(&m.fields[m.focus])
			return m, cmd, true
		case "down", "j":
			m.focus = min(m.focus+1, len(m.fields)-1)
		case "up", "k":
			m.focus = max(0, m.focus-1)
		}
		return m, nil, true
	}
	if m.mode == "servers" {
		switch k {
		case "q", "esc", "f2", "tab", "shift+tab", "left":
			m.mode = "editor"
		case "a":
			m.openServer(true)
		case "e", "enter":
			m.openServer(len(m.servers) == 0)
		case "j", "down":
			m.serverIndex = min(m.serverIndex+1, max(0, len(m.servers)-1))
		case "k", "up":
			m.serverIndex = max(0, m.serverIndex-1)
		case "[", "]", "ctrl+up", "ctrl+down":
			other := m.serverIndex + 1
			if k == "[" || k == "ctrl+up" {
				other = m.serverIndex - 1
			}
			if other >= 0 && other < len(m.servers) {
				m.servers[other], m.servers[m.serverIndex] = m.servers[m.serverIndex], m.servers[other]
				m.serverIndex = other
			}
		case " ":
			if len(m.servers) > 0 {
				m.servers[m.serverIndex].Disabled = !m.servers[m.serverIndex].Disabled
			}
		case "d":
			if len(m.servers) > 0 {
				i := m.serverIndex
				name := m.servers[i].Name
				m.preview("Удалить сервер "+name+" из черновика маршрута?", func() tea.Msg {
					remaining := slices.Delete(slices.Clone(m.servers), i, i+1)
					return result{serverData: remaining, returnMode: "servers", text: "Сервер удалён из черновика"}
				})
			}
		}
		return m, nil, true
	}
	if m.mode == "serverform" {
		f := &m.serverFields[m.serverFocus]
		if m.fieldEditing {
			cmd := m.updateFieldEdit(f, key)
			return m, cmd, true
		}
		switch k {
		case "q", "esc":
			m.mode = "servers"
			m.notice = ""
			return m, nil, true
		case "s", "ctrl+s":
			if e := m.storeServer(); e != nil {
				m.notice = e.Error()
			}
			return m, nil, true
		case "tab", "shift+tab":
			delta := 1
			if k == "shift+tab" {
				delta = -1
			}
			sections := len(serverFieldGroups)
			m.serverFocus = serverFieldGroups[(serverSection(m.serverFocus)+sections+delta)%sections][0]
		case "down", "j", "up", "k":
			delta := 1
			if k == "up" || k == "k" {
				delta = -1
			}
			fields := serverFieldGroups[serverSection(m.serverFocus)]
			m.serverFocus = fields[min(len(fields)-1, max(0, slices.Index(fields, m.serverFocus)+delta))]
		case " ", "enter":
			switch m.serverFocus {
			case serverRoleField:
				f.SetValue(cycle(f.Value(), "primary", "backup"))
				return m, nil, true
			case serverHealthField:
				f.SetValue(cycle(f.Value(), "inherit", "check", "nocheck"))
				return m, nil, true
			case serverStateField:
				f.SetValue(cycle(f.Value(), "enabled", "disabled"))
				return m, nil, true
			}
			if k == "enter" {
				cmd := m.startFieldEdit(f)
				return m, cmd, true
			}
		}
		return m, nil, true
	}
	return m, nil, false
}

func parseServerFields(v []string) (haproxy.Server, error) {
	port, err := strconv.Atoi(v[2])
	if err != nil || haproxy.ValidatePort(port) != nil {
		return haproxy.Server{}, fmt.Errorf("Порт сервера: введите число от 1 до 65535")
	}
	weight, err := optionalNumber(v[4])
	if err != nil || weight < 0 || weight > 256 {
		return haproxy.Server{}, fmt.Errorf("Вес: введите 0–256 или -")
	}
	fall, fallErr := optionalNumber(v[8])
	rise, riseErr := optionalNumber(v[9])
	if fallErr != nil || riseErr != nil || v[8] != "-" && fall < 1 || v[9] != "-" && rise < 1 {
		return haproxy.Server{}, fmt.Errorf("Fall и Rise: введите положительное число или -")
	}
	interval := v[7]
	if interval == "-" {
		interval = ""
	}
	server := haproxy.Server{Name: v[0], Address: v[1], Port: port, Weight: weight, WeightSet: v[4] != "-",
		Backup: v[serverRoleField] == "backup", HealthCheck: v[serverHealthField] == "check", CheckDisabled: v[serverHealthField] == "nocheck", Disabled: v[serverStateField] == "disabled",
		Interval: interval, Fall: fall, Rise: rise}
	return server, haproxy.ValidateServer(server)
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
