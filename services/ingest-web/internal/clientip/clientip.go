// Package clientip descobre o IP do cliente e o trunca antes de qualquer uso.
//
// O IP completo só existe na memória do request: o que sai do gateway (para o
// Umami ou para o limite de taxa) é sempre o resultado de Truncate.
package clientip

import (
	"errors"
	"net"
	"net/http"
	"net/netip"
	"strings"
)

// Truncate zera o último octeto do IPv4 e tudo depois do /48 no IPv6.
// Um IPv4 mapeado em IPv6 (::ffff:a.b.c.d) é tratado como IPv4.
func Truncate(a netip.Addr) netip.Addr {
	a = a.Unmap()
	bits := 48
	if a.Is4() {
		bits = 24
	}
	p, err := a.Prefix(bits)
	if err != nil {
		return netip.Addr{}
	}
	return p.Addr()
}

// Resolver acha o IP do cliente a partir do request.
type Resolver struct {
	// Trusted são as redes dos proxies (o Caddy) cujo X-Forwarded-For vale.
	Trusted []netip.Prefix
}

// ErrNoIP indica que o RemoteAddr não tem um IP válido.
var ErrNoIP = errors.New("clientip: endereço remoto inválido")

// FromRequest devolve o IP do cliente, ainda completo. Quem chama deve passar
// o resultado por Truncate antes de usá-lo fora do request.
//
// O X-Forwarded-For só é lido quando a conexão vem de um proxy confiável. Nele,
// o cliente é o primeiro endereço da direita para a esquerda que não é de um
// proxy confiável; entradas à esquerda dele podem ter sido forjadas.
func (r Resolver) FromRequest(req *http.Request) (netip.Addr, error) {
	remote, err := parseHostPort(req.RemoteAddr)
	if err != nil {
		return netip.Addr{}, ErrNoIP
	}
	if !r.trusted(remote) {
		return remote, nil
	}

	var hops []string
	for _, v := range req.Header.Values("X-Forwarded-For") {
		hops = append(hops, strings.Split(v, ",")...)
	}
	client := remote
	for i := len(hops) - 1; i >= 0; i-- {
		a, err := netip.ParseAddr(strings.TrimSpace(hops[i]))
		if err != nil {
			break
		}
		client = a.Unmap()
		if !r.trusted(client) {
			break
		}
	}
	return client, nil
}

func (r Resolver) trusted(a netip.Addr) bool {
	for _, p := range r.Trusted {
		if p.Contains(a) {
			return true
		}
	}
	return false
}

func parseHostPort(s string) (netip.Addr, error) {
	host, _, err := net.SplitHostPort(s)
	if err != nil {
		host = s
	}
	a, err := netip.ParseAddr(host)
	if err != nil {
		return netip.Addr{}, err
	}
	return a.Unmap(), nil
}
