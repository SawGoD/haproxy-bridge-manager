package tui

import (
	"errors"
	"github.com/SawGoD/haproxy-bridge/internal/monitor"
	"github.com/SawGoD/haproxy-bridge/internal/platform"
	"github.com/SawGoD/haproxy-bridge/internal/settings"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func sampleModel(t *testing.T) Model {
	t.Helper()
	dir := t.TempDir()
	cfg := settings.Default()
	cfg.General.ConfigPath = filepath.Join(dir, "haproxy.cfg")
	cfg.General.BackupDir = filepath.Join(dir, "backups")
	b, e := os.ReadFile("../../testdata/haproxy.cfg")
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(cfg.General.ConfigPath, b, 0600); e != nil {
		t.Fatal(e)
	}
	p := filepath.Join(dir, "config.toml")
	if e = cfg.Save(p); e != nil {
		t.Fatal(e)
	}
	return New(p)
}
func press(m Model, key tea.KeyMsg) Model { n, _ := m.Update(key); return n.(Model) }
func TestStructuredSettingsAndCancel(t *testing.T) {
	m := sampleModel(t)
	original := m.cfg.General.ConfigPath
	m = press(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'o'}})
	if len(m.prefs) == 0 || strings.Contains(m.View(), "config_path =") {
		t.Fatal("raw TOML shown")
	}
	m.prefs[0].input.SetValue("/tmp/changed.cfg")
	m = press(m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.cfg.General.ConfigPath != original || m.mode != "main" {
		t.Fatal("cancel changed settings")
	}
}

func TestPreferenceSectionsUseTab(t *testing.T) {
	m := sampleModel(t)
	m.beginPreferences(false)
	m = press(m, tea.KeyMsg{Type: tea.KeyTab})
	m = press(m, tea.KeyMsg{Type: tea.KeyTab})
	if m.prefs[m.prefIndex].section != "Docker" || m.prefs[m.prefIndex].label != "Имя контейнера" {
		t.Fatal(m.View())
	}
	m = press(m, tea.KeyMsg{Type: tea.KeyDown})
	if m.prefs[m.prefIndex].label != "Файл Compose" {
		t.Fatal(m.View())
	}
	m = press(m, tea.KeyMsg{Type: tea.KeyShiftTab})
	if m.prefs[m.prefIndex].section != "HAProxy" {
		t.Fatal(m.View())
	}
}

func TestApplyHelpOpensMethodAndPreservesRouteDraft(t *testing.T) {
	m := sampleModel(t)
	if err := m.s.Document.DeleteFrontend("de1_in"); err != nil {
		t.Fatal(err)
	}
	draft := m.s.Document.Bytes()
	next, _ := m.Update(result{err: platform.ErrRecreateRequired})
	m = next.(Model)
	if m.mode != "apply-help" || !strings.Contains(m.View(), "Нажмите o") {
		t.Fatal(m.View())
	}
	m = press(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'o'}})
	p := m.prefs[m.prefIndex]
	if m.mode != "settings" || p.section != "Docker" || p.label != "Способ применения" || p.input.Value() != "restart" {
		t.Fatal(m.View())
	}
	m = press(m, tea.KeyMsg{Type: tea.KeySpace})
	m = press(m, tea.KeyMsg{Type: tea.KeyCtrlS})
	cfg, err := settings.Load(m.settingsPath)
	if err != nil || cfg.Docker.ApplyMethod != "recreate" || m.s.Settings.Docker.ApplyMethod != "recreate" ||
		m.mode != "main" || !slices.Equal(draft, m.s.Document.Bytes()) {
		t.Fatalf("settings or draft lost: %v, mode=%s, Apply=%s", err, m.mode, cfg.Docker.ApplyMethod)
	}
}
func TestServerFormAndConfirmedDelete(t *testing.T) {
	m := sampleModel(t)
	m.openRoute(editRoute)
	before := slices.Clone(m.servers)
	m = press(m, tea.KeyMsg{Type: tea.KeyF2})
	if m.mode != "servers" {
		t.Fatal(m.mode)
	}
	m = press(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	if m.mode != "serverform" {
		t.Fatal(m.mode)
	}
	m.serverFields[0].SetValue("public6")
	m.serverFields[1].SetValue("2001:db8::1")
	m.serverFields[serverRoleField].SetValue("backup")
	if e := m.storeServer(); e != nil {
		t.Fatal(e)
	}
	if added := m.servers[2]; added.Name != "public6" || added.Address != "2001:db8::1" || added.Port != 443 || !added.Backup {
		t.Fatal(added)
	}
	m.serverIndex = 2
	m = press(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	if m.mode != "confirm" || len(m.servers) != 3 {
		t.Fatal("deleted without confirmation")
	}
	msg := m.action()
	n, _ := m.Update(msg)
	m = n.(Model)
	if m.mode != "servers" || !slices.Equal(m.servers, before) {
		t.Fatal("confirmed deletion not applied to editor", m.mode, m.servers)
	}
}
func TestLayoutSizes(t *testing.T) {
	for _, size := range [][2]int{{48, 16}, {80, 24}, {120, 36}, {150, 38}} {
		m := sampleModel(t)
		n, _ := m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		m = n.(Model)
		for _, mode := range []string{"main", "actions", "overview", "editor", "servers", "serverform", "backups", "settings", "wizard"} {
			switch mode {
			case "actions":
				m = press(m, tea.KeyMsg{Type: tea.KeyTab})
			case "overview":
				m.mode = "main"
				m = press(m, tea.KeyMsg{Type: tea.KeyEnter})
			case "editor":
				m.openRoute(editRoute)
			case "servers":
				m.mode = "servers"
			case "serverform":
				m.serverIndex = 0
				m.openServer(false)
			case "backups":
				m.mode = "backups"
			case "settings":
				m.beginPreferences(false)
			case "wizard":
				m.beginPreferences(true)
			}
			view := m.View()
			lines := strings.Split(view, "\n")
			if len(lines) > size[1] {
				t.Fatalf("%s exceeds height: %d>%d", mode, len(lines), size[1])
			}
			for _, line := range lines {
				if ansi.StringWidth(line) > size[0] {
					t.Fatalf("%s exceeds width: %d>%d", mode, ansi.StringWidth(line), size[0])
				}
			}
			if mode == "main" && !strings.Contains(view, "UNKNOWN") {
				t.Fatal("status hidden")
			}
		}
	}
}

func TestMainRouteMonitoringShowsFailoverAndDiagnostic(t *testing.T) {
	m := sampleModel(t)
	m.cfg.Monitoring.Socket = "/test/runtime.sock"
	snapshot, err := monitor.Parse(`# pxname,svname,status,scur,stot,econ,check_status,lastchg,act,bck,type,downtime,check_duration,last_chk
de1_out,BACKEND,UP,1,8,3,,90,0,1,1,12,,
de1_out,primary,DOWN,0,5,3,L4CON,22,0,0,2,22,14,Connection refused
de1_out,reserve,UP,1,3,0,L4OK,90,0,1,2,0,8,
usa1_out,BACKEND,DOWN,0,0,0,,30,0,0,1,30,,
`)
	if err != nil {
		t.Fatal(err)
	}
	next, _ := m.Update(statsMsg{snapshot: snapshot, path: m.cfg.Monitoring.Socket})
	m = next.(Model)
	m.width, m.height = 120, 36
	view := ansi.Strip(m.View())
	for _, part := range []string{"UP · резерв", "Обслуживают: reserve", "основной", "резервный", "DOWN", "Аптайм: 1m30s", "Простой backend: 12s", "Сессии: 0 сейчас / 5 всего", "L4CON · 14 мс", "Connection refused"} {
		if !strings.Contains(view, part) {
			t.Fatalf("missing %q from %s", part, m.View())
		}
	}
	for _, line := range strings.Split(view, "\n") {
		if strings.Contains(line, "› de1_in") && !strings.Contains(line, "10443  UP · резерв") {
			t.Fatal(line)
		}
	}
	idle := []monitor.Server{{Name: "primary"}, {Name: "reserve", Backup: true}}
	if got := m.routeSessionLine(snapshot.Backends["de1_out"], idle); got != "Нет сессий · доступен резервный" {
		t.Fatal(got)
	}
	if got := m.routeSessionLine(monitor.Backend{Status: "UP", ServerCountsKnown: true, Primary: 1}, idle); got != "Нет сессий · доступен основной" {
		t.Fatal(got)
	}
	next, _ = m.Update(tea.WindowSizeMsg{Width: 48, Height: 16})
	m = next.(Model)
	for i := 0; i < 4; i++ {
		m = press(m, tea.KeyMsg{Type: tea.KeyPgDown})
		if strings.Contains(m.View(), "Connection refused") {
			break
		}
	}
	if !strings.Contains(m.View(), "Connection refused") || m.cursor != 0 {
		t.Fatal(m.View())
	}
	m = press(m, tea.KeyMsg{Type: tea.KeyDown})
	if !strings.Contains(ansi.Strip(m.View()), "usa1_out  DOWN · 30s") || m.detailOffset != 0 {
		t.Fatal(m.View())
	}
}

func TestActionMenuAndPreviewCancel(t *testing.T) {
	m := sampleModel(t)
	m = press(m, tea.KeyMsg{Type: tea.KeyDown})
	want := m.rows()[m.cursor].Name
	m = press(m, tea.KeyMsg{Type: tea.KeyTab})
	if m.mode != "actions" || !strings.Contains(m.View(), "Редактировать маршрут") {
		t.Fatal(m.View())
	}
	m = press(m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.mode != "editor" || m.editing != want {
		t.Fatal(m.mode, m.editing)
	}
	m.fields[5].SetValue("1m")
	m = press(m, tea.KeyMsg{Type: tea.KeyCtrlS})
	if m.mode != "confirm" {
		t.Fatal(m.mode, m.notice)
	}
	m = press(m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.mode != "editor" || m.fields[5].Value() != "1m" {
		t.Fatal("cancel lost the editor draft")
	}
}

func TestEditorNavigationAndExplicitFieldEdit(t *testing.T) {
	m := sampleModel(t)
	key := func(keyType tea.KeyType) { m = press(m, tea.KeyMsg{Type: keyType}) }
	letter := func(r rune) { m = press(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}}) }
	key(tea.KeyEnter)
	if m.mode != "overview" || !strings.Contains(m.View(), "Общее") {
		t.Fatal(m.View())
	}
	key(tea.KeyTab)
	if m.overviewTab != 1 || !strings.Contains(m.View(), "de1-public") {
		t.Fatal(m.View())
	}
	key(tea.KeyShiftTab)
	letter('e')
	name := m.fields[0].Value()
	if m.mode != "editor" || m.fields[0].Focused() || m.fieldEditing {
		t.Fatal("route opened in input mode")
	}
	letter('q')
	if m.mode != "overview" {
		t.Fatal(m.mode)
	}
	letter('q')
	if m.mode != "main" || m.fields[0].Value() != name {
		t.Fatal("q did not leave the route editor")
	}
	letter('e')
	key(tea.KeyEnter)
	letter('q')
	if !m.fieldEditing || m.fields[0].Value() != name+"q" {
		t.Fatal("explicit input did not accept q")
	}
	key(tea.KeyEsc)
	if m.mode != "editor" || m.fieldEditing || m.fields[0].Value() != name {
		t.Fatal("escape did not restore the field")
	}
	key(tea.KeyTab)
	key(tea.KeyEnter)
	if m.mode != "serverform" || m.fieldEditing || m.serverFields[m.serverFocus].Focused() {
		t.Fatal("server opened in input mode")
	}
	key(tea.KeyEnter)
	key(tea.KeyCtrlU)
	for _, r := range "2001:db8::5" {
		letter(r)
	}
	key(tea.KeyEnter)
	letter('s')
	if m.mode != "servers" || m.servers[0].Address != "2001:db8::5" {
		t.Fatal(m.mode, m.notice)
	}
	key(tea.KeyEnter)
	letter('q')
	if m.mode != "servers" {
		t.Fatal(m.mode)
	}
	letter('q')
	if m.mode != "editor" {
		t.Fatal(m.mode)
	}
	letter('s')
	if m.mode != "confirm" {
		t.Fatal(m.mode, m.notice)
	}
	letter('q')
	if m.mode != "editor" || m.fieldEditing || m.servers[0].Address != "2001:db8::5" {
		t.Fatal("leaving preview lost the draft")
	}
}
func TestDiffShowsOnlyChangeContext(t *testing.T) {
	old := strings.Repeat("unchanged\n", 20) + "old\n" + strings.Repeat("tail\n", 20)
	next := strings.Replace(old, "old\n", "new\n", 1)
	got := diff([]byte(old), []byte(next))
	if !strings.Contains(got, "-old\n+new") || strings.Count(got, "unchanged") > 3 {
		t.Fatal(got)
	}
}

func TestBackupPreviewSeparatesRestore(t *testing.T) {
	m := sampleModel(t)
	if _, err := m.s.Backups.Create(m.s.Path); err != nil {
		t.Fatal(err)
	}
	key := func(r rune) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}} }
	m = press(m, key('b'))
	m = press(m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.mode != "backup-preview" || m.action != nil {
		t.Fatal("preview unexpectedly armed restore")
	}
	m = press(m, key('r'))
	if m.mode != "confirm" || !strings.Contains(m.View(), "Восстановление из бэкапа") {
		t.Fatal(m.View())
	}
	m = press(m, key('q'))
	if m.mode != "backups" || m.action != nil {
		t.Fatal("cancel did not return to backups")
	}
}

func TestOperationProgressIsHonest(t *testing.T) {
	o := operationState{operationStarted: operationStarted{success: "Конфигурация применена"}, steps: []string{"Валидация", "Проверка портов"}}
	pending := ansi.Strip(o.content())
	if !strings.Contains(pending, "OK   Валидация") || !strings.Contains(pending, "В работе   Проверка портов") {
		t.Fatal(pending)
	}
	o.done, o.err = true, errors.New("порт не ответил")
	failed := ansi.Strip(o.content())
	if !strings.Contains(failed, "Ошибка   Проверка портов") || !strings.Contains(failed, "порт не ответил") || strings.Contains(failed, o.success) {
		t.Fatal(failed)
	}
}
