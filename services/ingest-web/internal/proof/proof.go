// Package proof emite e confere a prova de execução de JavaScript (#25).
//
// O script pede um desafio em GET /c, recebe um token assinado pelo gateway e
// procura um nonce tal que SHA-256(token + ":" + nonce) comece com Bits bits
// zero. Cada evento leva o token e o nonce. Um POST feito direto, sem passar
// pelo desafio, não tem token válido e é descartado.
//
// Não é uma barreira impossível de contornar: quem reimplementar o fluxo fora
// do navegador consegue a prova. O objetivo é tirar o tráfego barato (curl,
// scripts que só repetem um POST) e encarecer o resto.
package proof

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"math/bits"
	"strconv"
	"strings"
	"time"
)

// Limites de tamanho do que vem do cliente.
const (
	tokenLen = 8 + 8 + 16 // timestamp, aleatório e HMAC truncado
	maxNonce = 20
	// clockSkew tolera um relógio do gateway levemente adiantado em outra réplica.
	clockSkew = time.Minute
	// domain separa este HMAC de qualquer outro uso da mesma chave.
	domain = "stellytics-proof-v1"
)

// Erros de Verify. Todos levam ao descarte do evento.
var (
	ErrMissing   = errors.New("proof: sem prova")
	ErrMalformed = errors.New("proof: prova mal formada")
	ErrSignature = errors.New("proof: assinatura inválida")
	ErrExpired   = errors.New("proof: token expirado")
	ErrWork      = errors.New("proof: trabalho insuficiente")
)

// Challenge é a resposta de GET /c.
type Challenge struct {
	Token string `json:"t"`
	Bits  int    `json:"d"`
}

// Issuer emite e confere desafios com uma chave secreta.
type Issuer struct {
	key  []byte
	ttl  time.Duration
	bits int
	now  func() time.Time
}

// New cria um Issuer. key deve ter pelo menos 32 bytes.
func New(key []byte, ttl time.Duration, bits int) *Issuer {
	return &Issuer{key: key, ttl: ttl, bits: bits, now: time.Now}
}

// WithClock troca o relógio (para testes) e devolve o próprio Issuer.
func (i *Issuer) WithClock(now func() time.Time) *Issuer {
	i.now = now
	return i
}

// Challenge emite um token para website (UUID já em minúsculas).
func (i *Issuer) Challenge(website string) Challenge {
	raw := make([]byte, tokenLen)
	binary.BigEndian.PutUint64(raw[0:8], uint64(i.now().Unix()))
	_, _ = rand.Read(raw[8:16])
	copy(raw[16:], i.mac(raw[0:16], website))
	return Challenge{Token: base64.RawURLEncoding.EncodeToString(raw), Bits: i.bits}
}

// Verify confere o token e o nonce enviados com um evento de website.
func (i *Issuer) Verify(website, token, nonce string) error {
	if token == "" && nonce == "" {
		return ErrMissing
	}
	if len(token) != base64.RawURLEncoding.EncodedLen(tokenLen) || nonce == "" || len(nonce) > maxNonce {
		return ErrMalformed
	}
	if _, err := strconv.ParseUint(nonce, 10, 64); err != nil {
		return ErrMalformed
	}
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return ErrMalformed
	}
	if !hmac.Equal(raw[16:], i.mac(raw[0:16], website)) {
		return ErrSignature
	}
	issued := time.Unix(int64(binary.BigEndian.Uint64(raw[0:8])), 0)
	now := i.now()
	if now.Sub(issued) > i.ttl || issued.Sub(now) > clockSkew {
		return ErrExpired
	}
	if !Solved(token, nonce, i.bits) {
		return ErrWork
	}
	return nil
}

func (i *Issuer) mac(head []byte, website string) []byte {
	h := hmac.New(sha256.New, i.key)
	h.Write([]byte(domain))
	h.Write(head)
	h.Write([]byte(strings.ToLower(website)))
	return h.Sum(nil)[:16]
}

// Solved diz se SHA-256(token + ":" + nonce) começa com pelo menos n bits zero.
func Solved(token, nonce string, n int) bool {
	sum := sha256.Sum256([]byte(token + ":" + nonce))
	return LeadingZeros(sum[:]) >= n
}

// LeadingZeros conta os bits zero no começo de b.
func LeadingZeros(b []byte) int {
	n := 0
	for _, c := range b {
		if c != 0 {
			return n + bits.LeadingZeros8(c)
		}
		n += 8
	}
	return n
}

// Solve acha um nonce para token, como o script faz no navegador. Usado nos
// testes e no teste ponta a ponta.
func Solve(token string, n int) string {
	for i := uint64(0); ; i++ {
		nonce := strconv.FormatUint(i, 10)
		if Solved(token, nonce, n) {
			return nonce
		}
	}
}
