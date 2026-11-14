// Package routenorm normaliza rotas HTTP com as regras fixas do RFP (#32).
//
// Contrato no spec do E5 (docs/specs/e5-pipeline-de-api.md, seção 3): o
// http.route enviado pelo cliente tem prioridade; sem ele, cada segmento do
// caminho que for UUID, endereço Stellar, hash hexadecimal ou número vira um
// marcador, para que /accounts/GABC.../payments/ e /accounts/GXYZ.../payments
// contem como a mesma rota.
package routenorm

import (
	"net/url"
	"strings"
)

// Marcadores que substituem os segmentos variáveis.
const (
	UUID    = "{uuid}"
	Address = "{address}"
	Hash    = "{hash}"
	ID      = "{id}"
)

// MaxLen é o tamanho máximo, em bytes, de uma rota gravada.
const MaxLen = 256

// minHashLen é o mínimo de dígitos hexadecimais de um {hash} (D8 do spec),
// para não trocar palavras como "cafe" ou "add".
const minHashLen = 16

// Route devolve a rota de uma requisição. clientRoute é o http.route enviado
// pelo cliente (o template do framework) e tem prioridade; path é o url.path
// ou o http.target, que pode trazer query string ou ser uma URL completa.
func Route(clientRoute, path string) string {
	if clientRoute != "" {
		return truncate(clientRoute)
	}
	return Path(path)
}

// Path normaliza um caminho: tira query string e fragmento, troca os
// segmentos variáveis por marcadores, junta barras repetidas e tira a barra
// final.
func Path(path string) string {
	if i := strings.IndexAny(path, "?#"); i >= 0 {
		path = path[:i]
	}
	// URL completa (http.target antigo ou cliente mal configurado): só o caminho.
	if strings.Contains(path, "://") {
		if u, err := url.Parse(path); err == nil {
			path = u.EscapedPath()
		}
	}

	var b strings.Builder
	for _, seg := range strings.Split(path, "/") {
		if seg == "" {
			continue
		}
		b.WriteByte('/')
		b.WriteString(segment(seg))
	}
	if b.Len() == 0 {
		return "/"
	}
	out := b.String()
	// Rota longa demais: corta no fim do último segmento inteiro que cabe, para
	// não deixar um pedaço de segmento que pareça outro marcador.
	if len(out) > MaxLen {
		if i := strings.LastIndexByte(out[:MaxLen+1], '/'); i > 0 {
			return out[:i]
		}
	}
	return truncate(out)
}

// segment troca o segmento pelo primeiro marcador que casar, na ordem do spec.
func segment(s string) string {
	switch {
	case isUUID(s):
		return UUID
	case isStellarAddress(s):
		return Address
	case isHexHash(s):
		return Hash
	case isDigits(s):
		return ID
	}
	return s
}

func isUUID(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i := 0; i < len(s); i++ {
		switch i {
		case 8, 13, 18, 23:
			if s[i] != '-' {
				return false
			}
		default:
			if !isHex(s[i]) {
				return false
			}
		}
	}
	return true
}

// isStellarAddress confere o formato StrKey: G (conta) ou C (contrato) com 56
// caracteres, ou M (conta muxed) com 69, em base32 maiúsculo. O checksum não é
// conferido (spec, seção 3).
func isStellarAddress(s string) bool {
	switch {
	case len(s) == 56 && (s[0] == 'G' || s[0] == 'C'):
	case len(s) == 69 && s[0] == 'M':
	default:
		return false
	}
	for i := 1; i < len(s); i++ {
		c := s[i]
		if !(c >= 'A' && c <= 'Z' || c >= '2' && c <= '7') {
			return false
		}
	}
	return true
}

func isHexHash(s string) bool {
	if len(s) > 2 && (s[:2] == "0x" || s[:2] == "0X") {
		s = s[2:]
	}
	if len(s) < minHashLen {
		return false
	}
	for i := 0; i < len(s); i++ {
		if !isHex(s[i]) {
			return false
		}
	}
	return true
}

func isDigits(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return s != ""
}

func isHex(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F'
}

// truncate corta a rota em MaxLen bytes sem partir um caractere UTF-8.
func truncate(s string) string {
	if len(s) <= MaxLen {
		return s
	}
	s = s[:MaxLen]
	for len(s) > 0 && s[len(s)-1]&0xC0 == 0x80 {
		s = s[:len(s)-1]
	}
	if len(s) > 0 && s[len(s)-1] >= 0xC0 {
		s = s[:len(s)-1]
	}
	return s
}
