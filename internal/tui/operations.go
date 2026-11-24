package tui

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/SawGoD/haproxy-bridge/internal/app"
	"github.com/SawGoD/haproxy-bridge/internal/haproxy"
	"github.com/SawGoD/haproxy-bridge/internal/network"
	tea "github.com/charmbracelet/bubbletea"
)

type operationStarted struct {
	title, path, success string
	started              time.Time
	events               <-chan tea.Msg
}

type operationStep string
type operationFinished struct{ err error }

type operationState struct {
	operationStarted
	steps   []string
	elapsed time.Duration
	done    bool
	err     error
}

func followOperation(events <-chan tea.Msg) tea.Cmd {
	return func() tea.Msg { return <-events }
}

func (m Model) validateCmd() tea.Cmd {
	return func() tea.Msg {
		return startOperation("Проверка конфигурации", m.s.Path, "Конфигурация корректна", func(step func(string)) error {
			return m.s.ValidateWithProgress(m.s.Document.Bytes(), step)
		})
	}
}

func startOperation(title, path, success string, run func(func(string)) error) operationStarted {
	events := make(chan tea.Msg, 1)
	started := time.Now()
	go func() {
		err := run(func(label string) { events <- operationStep(label) })
		events <- operationFinished{err}
		close(events)
	}()
	return operationStarted{title, path, success, started, events}
}

func startApply(s *app.Session, save bool) operationStarted {
	success := "Конфигурация применена"
	if save {
		success = "Сохранено и применено"
	}
	return startOperation("Применение конфигурации", s.Path, success, func(step func(string)) error {
		if save {
			step("Проверка способа применения")
			if err := s.CheckApplyMethod(); err != nil {
				return err
			}
			step("Валидация, резервная копия и запись файла")
			if err := s.Save(); err != nil {
				return err
			}
		}
		return s.ApplyWithProgress(step)
	})
}

func (o operationState) content() string {
	var lines []string
	if o.done {
		label, style := o.success, statusUp
		if o.err != nil {
			label, style = "Операция завершилась с ошибкой", statusDown
		}
		lines = append(lines, style.Bold(true).Padding(0, 1).Render(label), "Время: "+o.elapsed.Round(time.Millisecond).String(), "")
	}
	for i, step := range o.steps {
		label, style := "OK", statusUp
		if i == len(o.steps)-1 {
			switch {
			case !o.done:
				label, style = "В работе", statusWarning
			case o.err != nil:
				label, style = "Ошибка", statusDown
			}
		}
		lines = append(lines, style.Bold(true).Padding(0, 1).Render(label)+"  "+step)
	}
	if o.err != nil {
		lines = append(lines, "", warning.Render("Причина"))
		for _, line := range strings.Split(o.err.Error(), "\n") {
			lines = append(lines, oneLine(line))
		}
	}
	return strings.Join(lines, "\n")
}

func (m Model) operationView() string {
	if m.busy {
		return m.screen(m.operation.title, m.operation.path, m.operation.content(), hint("", "Дождитесь завершения"))
	}
	help := hint("↑↓ ←→", "прокрутка") + "   " + hint("Q / Esc", "назад")
	if m.operation.success == "Конфигурация корректна" {
		help += "\n" + hint("R", "повторить") + "   " + hint("P", "применить")
	}
	return m.screen(m.operation.title, m.operation.path, m.view.View(), help)
}

type probeResult struct {
	route, kind, back string
	checks            []serverProbe
	command           tea.Cmd
}

type serverProbe struct {
	name string
	network.Result
}

func (m Model) probeCmd(route haproxy.Frontend, servers []haproxy.Server, tls bool, back string) tea.Cmd {
	kind := "TCP"
	if tls {
		kind = "TLS"
	}
	var cmd tea.Cmd
	cmd = func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), time.Duration(m.cfg.Network.ConnectTimeout)*time.Second)
		defer cancel()
		checks := make([]serverProbe, len(servers))
		done := make(chan struct{}, len(servers))
		for i, server := range servers {
			go func() {
				address := net.JoinHostPort(server.Address, strconv.Itoa(server.Port))
				checks[i] = serverProbe{server.Name, network.Probe(ctx, address, m.cfg.Network.SNI, tls)}
				done <- struct{}{}
			}()
		}
		for range servers {
			<-done
		}
		return probeResult{route.Name, kind, back, checks, cmd}
	}
	return cmd
}

func (p probeResult) content() string {
	var rows []string
	passed := 0
	for _, check := range p.checks {
		label := "OK"
		if !check.TCP || (p.kind == "TLS" && !check.CertificateValid) {
			label = "DOWN"
			if strings.Contains(strings.ToLower(check.Error), "timeout") || strings.Contains(check.Error, "deadline exceeded") {
				label = "TIMEOUT"
			}
		} else {
			passed++
		}
		style := statusUp
		if label != "OK" {
			style = statusDown
		}
		rows = append(rows, accent.Bold(true).Render(check.name)+"  "+check.Address,
			"  "+p.kind+" "+style.Bold(true).Render(label)+"  TCP: "+check.Latency.Round(time.Millisecond).String())
		if check.TLSVersion != "" {
			rows = append(rows, "  "+check.TLSVersion+"  CN: "+oneLine(check.CN))
			if len(check.SAN) > 0 {
				rows = append(rows, "  SAN: "+oneLine(strings.Join(check.SAN, ", ")))
			}
		}
		if check.Error != "" {
			rows = append(rows, "  "+warning.Render(oneLine(check.Error)))
		}
		rows = append(rows, "")
	}
	style := statusUp
	if passed != len(p.checks) || passed == 0 {
		style = statusWarning
	}
	return style.Bold(true).Padding(0, 1).Render(fmt.Sprintf("Проверка завершена · %d / %d успешно", passed, len(p.checks))) + "\n\n" + strings.Join(rows, "\n")
}
