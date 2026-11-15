package main

import (
	"fmt"
	"net"
	"net/url"
	"strings"
)

// checkTargets recusa alvos que não sejam loopback, a menos que allowRemote
// seja true: o setup, o teardown e as cargas escrevem e apagam dados, e não
// podem apontar por engano para um banco ou serviço de produção. Imprime host
// e banco de cada alvo antes de qualquer ação.
func checkTargets(allowRemote bool, targets map[string]string) error {
	for name, raw := range targets {
		u, err := url.Parse(raw)
		if err != nil || u.Hostname() == "" {
			return fmt.Errorf("%s: URL inválida %q", name, redact(raw))
		}
		fmt.Printf("alvo %-12s host=%s banco=%s\n", name, u.Host, strings.TrimPrefix(u.Path, "/"))
		if !isLoopback(u.Hostname()) && !allowRemote {
			return fmt.Errorf("%s aponta para %q, que não é loopback; use -allow-remote se for de propósito", name, u.Hostname())
		}
	}
	return nil
}

func redact(s string) string {
	if u, err := url.Parse(s); err == nil && u.User != nil {
		u.User = url.User("***")
		return u.String()
	}
	return s
}

func isLoopback(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
