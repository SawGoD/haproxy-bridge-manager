package testfixture

// HAProxyConfig is a synthetic four-route configuration for parser and TUI tests.
const HAProxyConfig = `# Lossless parser fixture: comments and unknown directives are intentional.
global
	daemon
	unknown-global-directive preserved

defaults
	mode tcp
	timeout connect 5s
	timeout client 1m
	timeout server 1m

frontend de1_in
	bind 127.0.0.1:10443
	option tcplog
	default_backend de1_out

backend de1_out
	unknown-backend-directive keep-me
	server de1-public 192.0.2.10:443 check maxconn 32
	server de1-netbird 192.0.2.11:443 check maxconn 32

frontend usa1_in
	bind 127.0.0.1:10444
	default_backend usa1_out

backend usa1_out
	server usa1-public 198.51.100.10:443 check
	server usa1-netbird 198.51.100.11:443 check

frontend lv1_in
	bind 127.0.0.1:10445
	default_backend lv1_out

backend lv1_out
	# lv1 is retained by targeted mutations.
	server lv1-public 203.0.113.10:443 check
	server lv1-netbird 203.0.113.11:443 check

frontend se1_in
	bind 127.0.0.1:10446
	default_backend se1_out

backend se1_out
	server se1-public 203.0.113.20:443 check
	server se1-netbird 203.0.113.21:443 check
`
