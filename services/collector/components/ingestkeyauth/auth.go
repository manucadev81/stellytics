// Package ingestkeyauth é a extensão de autenticação do coletor (#30).
//
// Contrato no spec do E2, seção 5, e no spec do E5, seção 1. O cliente manda a
// chave de ingestão em "Authorization: Bearer ntf_<prefixo>_<segredo>" no OTLP
// (HTTP ou gRPC). A extensão busca a chave pelo prefixo na visão
// ingest_key_active (usuário notify_collector), compara o SHA-256 em tempo
// constante e põe o tenant e o serviço da chave no contexto do pedido. Nenhum
// atributo enviado pelo cliente muda isso. Chave inválida, revogada, de
// serviço arquivado ou de tenant que não está ativo é recusada: o receiver
// responde 401 (HTTP) ou Unauthenticated (gRPC).
package ingestkeyauth

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"go.opentelemetry.io/collector/client"
	"go.opentelemetry.io/collector/extension/extensionauth"
	"go.uber.org/zap"
)

// Atributos que a extensão põe em client.Info.Auth.
const (
	AttrTenantID  = "tenant.id"
	AttrServiceID = "api_service.id"
	AttrPrefix    = "ingest_key.prefix"
)

// keyPattern é o formato da chave (spec do E2, seção 5).
var keyPattern = regexp.MustCompile(`^ntf_([a-z2-7]{8})_[A-Za-z0-9_-]{43}$`)

// errUnauthenticated é a única resposta de recusa, para não dar pista de qual
// parte da chave estava errada.
var errUnauthenticated = errors.New("chave de ingestão inválida")

// maxCached limita o cache, que também guarda respostas negativas.
const maxCached = 10_000

// Key é uma chave ativa como a visão ingest_key_active a devolve.
type Key struct {
	Hash      []byte
	TenantID  string
	ServiceID string
}

// Store busca a chave ativa pelo prefixo. ok é falso quando não há chave
// ativa com esse prefixo.
type Store interface {
	Lookup(ctx context.Context, prefix string) (key Key, ok bool, err error)
}

// Toucher registra o último uso de uma chave ativa (ingest_key.last_used_on).
// O Store do Postgres o implementa; um Store sem Touch simplesmente não grava.
type Toucher interface {
	Touch(ctx context.Context, prefix string) error
}

const (
	// touchTimeout limita a escrita em segundo plano.
	touchTimeout = 5 * time.Second
	// touchRetry é quanto esperar antes de tentar de novo depois de uma falha.
	touchRetry = 5 * time.Minute
)

// authenticator implementa extensionauth.Server.
type authenticator struct {
	store  Store
	ttl    time.Duration
	now    func() time.Time
	logger *zap.Logger

	mu    sync.Mutex
	cache map[[sha256.Size]byte]entry
	// nextTouch guarda, por prefixo, quando o próximo registro de último uso é
	// permitido: o começo do dia UTC seguinte depois de um sucesso, ou
	// touchRetry depois de uma falha. Só chaves autenticadas entram aqui, então
	// o mapa não cresce com tráfego inválido.
	nextTouch map[string]time.Time
}

type entry struct {
	key     Key
	ok      bool
	expires time.Time
}

// NewAuthenticator cria o autenticador sobre um Store qualquer. A extensão usa
// o Store do Postgres; os testes, um Store em memória.
func NewAuthenticator(store Store, ttl time.Duration, logger *zap.Logger) extensionauth.Server {
	return newAuthenticator(store, ttl, logger)
}

func newAuthenticator(store Store, ttl time.Duration, logger *zap.Logger) *authenticator {
	return &authenticator{store: store, ttl: ttl, now: time.Now, logger: logger, cache: make(map[[sha256.Size]byte]entry), nextTouch: make(map[string]time.Time)}
}

// Authenticate valida a chave e devolve o contexto com o tenant e o serviço.
func (a *authenticator) Authenticate(ctx context.Context, headers map[string][]string) (context.Context, error) {
	raw, ok := bearer(headers)
	if !ok {
		return ctx, errUnauthenticated
	}
	m := keyPattern.FindStringSubmatch(raw)
	if m == nil {
		return ctx, errUnauthenticated
	}
	prefix := m[1]
	hash := sha256.Sum256([]byte(raw))

	key, ok, err := a.lookup(ctx, prefix, hash)
	if err != nil {
		// Só o prefixo vai para o log; a chave completa nunca.
		a.logger.Error("consulta à ingest_key_active falhou", zap.String("prefix", prefix), zap.Error(err))
		return ctx, errUnauthenticated
	}
	if !ok {
		return ctx, errUnauthenticated
	}

	a.touch(prefix)

	info := client.FromContext(ctx)
	info.Auth = authData{tenantID: key.TenantID, serviceID: key.ServiceID, prefix: prefix}
	return client.NewContext(ctx, info), nil
}

// touch registra o último uso da chave em segundo plano, no máximo uma vez por
// chave e por dia UTC neste processo. Fora do caminho do pedido: o pedido não
// espera nem vê o resultado. Falha só vira log com o nome do erro (nunca a
// chave, nem a mensagem do driver) e uma nova tentativa depois de touchRetry.
func (a *authenticator) touch(prefix string) {
	toucher, ok := a.store.(Toucher)
	if !ok {
		return
	}
	now := a.now()
	a.mu.Lock()
	if next, seen := a.nextTouch[prefix]; seen && now.Before(next) {
		a.mu.Unlock()
		return
	}
	if len(a.nextTouch) >= maxCached {
		clear(a.nextTouch)
	}
	a.nextTouch[prefix] = nextUTCDay(now)
	a.mu.Unlock()

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), touchTimeout)
		defer cancel()
		if err := toucher.Touch(ctx, prefix); err != nil {
			a.logger.Warn("registro do último uso da chave falhou", zap.String("prefix", prefix), zap.String("erro", fmt.Sprintf("%T", err)))
			a.mu.Lock()
			a.nextTouch[prefix] = now.Add(touchRetry)
			a.mu.Unlock()
		}
	}()
}

// nextUTCDay é o começo do dia UTC depois de t.
func nextUTCDay(t time.Time) time.Time {
	y, m, d := t.UTC().Date()
	return time.Date(y, m, d+1, 0, 0, 0, 0, time.UTC)
}

// lookup consulta o cache pelo hash da chave completa e, sem resposta válida
// em cache, o banco. Erros do banco não ficam em cache.
func (a *authenticator) lookup(ctx context.Context, prefix string, hash [sha256.Size]byte) (Key, bool, error) {
	now := a.now()
	a.mu.Lock()
	e, cached := a.cache[hash]
	a.mu.Unlock()
	if cached && now.Before(e.expires) {
		return e.key, e.ok, nil
	}

	key, found, err := a.store.Lookup(ctx, prefix)
	if err != nil {
		return Key{}, false, err
	}
	ok := found && subtle.ConstantTimeCompare(key.Hash, hash[:]) == 1
	if !ok {
		key = Key{}
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.cache) >= maxCached {
		for k, v := range a.cache {
			if !now.Before(v.expires) {
				delete(a.cache, k)
			}
		}
		if len(a.cache) >= maxCached {
			clear(a.cache)
		}
	}
	a.cache[hash] = entry{key: key, ok: ok, expires: now.Add(a.ttl)}
	return key, ok, nil
}

// bearer acha o "Authorization: Bearer <chave>". No HTTP o header vem com a
// grafia canônica; no gRPC, a metadata vem em minúsculas.
func bearer(headers map[string][]string) (string, bool) {
	for name, values := range headers {
		if !strings.EqualFold(name, "authorization") || len(values) != 1 {
			continue
		}
		scheme, token, found := strings.Cut(values[0], " ")
		if found && strings.EqualFold(scheme, "bearer") && token != "" {
			return strings.TrimSpace(token), true
		}
	}
	return "", false
}

// authData é o que fica em client.Info.Auth para os componentes seguintes.
type authData struct {
	tenantID, serviceID, prefix string
}

func (d authData) GetAttribute(name string) any {
	switch name {
	case AttrTenantID:
		return d.tenantID
	case AttrServiceID:
		return d.serviceID
	case AttrPrefix:
		return d.prefix
	}
	return nil
}

func (authData) GetAttributeNames() []string {
	return []string{AttrTenantID, AttrServiceID, AttrPrefix}
}
