package main

import (
	"flag"
	"fmt"
	"github.com/SawGoD/haproxy-bridge/internal/settings"
	"github.com/SawGoD/haproxy-bridge/internal/tui"
	tea "github.com/charmbracelet/bubbletea"
	"os"
)

var version = "dev"

func main() {
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
