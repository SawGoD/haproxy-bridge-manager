package main

import (
	"flag"
	"fmt"
	"github.com/SawGoD/haproxy-bridge/internal/settings"
	"github.com/SawGoD/haproxy-bridge/internal/tui"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
	"os"
	"strings"
)

var version = "dev"

func main() {
	if strings.Contains(os.Getenv("TERM"), "256color") && os.Getenv("NO_COLOR") == "" {
		lipgloss.SetColorProfile(termenv.ANSI256)
	}
	p, e := settings.Path()
	if e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
	path := flag.String("settings", p, "Application TOML settings path")
	v := flag.Bool("version", false, "Print version")
	flag.Parse()
	if *v {
		fmt.Println("haproxy-bridge", version)
		return
	}
	if _, e = tea.NewProgram(tui.New(*path), tea.WithAltScreen()).Run(); e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}
