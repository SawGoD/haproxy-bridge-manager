package network

import (
	"context"
	"crypto/tls"
	"net"
	"time"
)

type Result struct {
	Address          string
	TCP              bool
	Latency          time.Duration
	TLSVersion, CN   string
	SAN              []string
	CertificateValid bool
	Error            string
}

func Probe(ctx context.Context, address, sni string, tlsEnabled bool) Result {
	r := Result{Address: address}
	start := time.Now()
	c, e := (&net.Dialer{}).DialContext(ctx, "tcp", address)
	r.Latency = time.Since(start)
	if e != nil {
		r.Error = e.Error()
		return r
	}
	r.TCP = true
	if !tlsEnabled {
		c.Close()
		return r
	}
	if sni == "" {
		sni, _, _ = net.SplitHostPort(address)
	}
	t := tls.Client(c, &tls.Config{ServerName: sni, MinVersion: tls.VersionTLS12})
	e = t.HandshakeContext(ctx)
	if e == nil {
		r.CertificateValid = true
		state := t.ConnectionState()
		r.TLSVersion = tls.VersionName(state.Version)
		if len(state.PeerCertificates) > 0 {
			r.CN = state.PeerCertificates[0].Subject.CommonName
			r.SAN = state.PeerCertificates[0].DNSNames
		}
		t.Close()
		return r
	}
	r.Error = e.Error()
	t.Close()
	c, e = (&net.Dialer{}).DialContext(ctx, "tcp", address)
	if e != nil {
		return r
	}
	diagnostic := tls.Client(c, &tls.Config{ServerName: sni, InsecureSkipVerify: true, MinVersion: tls.VersionTLS12})
	defer diagnostic.Close()
	if diagnostic.HandshakeContext(ctx) == nil {
		state := diagnostic.ConnectionState()
		r.TLSVersion = tls.VersionName(state.Version)
		if len(state.PeerCertificates) > 0 {
			r.CN = state.PeerCertificates[0].Subject.CommonName
			r.SAN = state.PeerCertificates[0].DNSNames
		}
	}
	return r
}
