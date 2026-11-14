package ingestkeyauth

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/extension"
	"go.opentelemetry.io/collector/extension/extensionauth"
)

// Type é o nome da extensão na configuração do coletor.
var Type = component.MustNewType("ingestkeyauth")

// Config da extensão.
type Config struct {
	// DatabaseURL aponta para o banco notify com o usuário notify_collector.
	DatabaseURL string `mapstructure:"database_url"`
	// CacheTTL é quanto tempo uma resposta (positiva ou negativa) fica em
	// memória. No máximo 60 s, para uma revogação valer em até 1 minuto (#59).
	CacheTTL time.Duration `mapstructure:"cache_ttl"`
}

// Validate implementa component.ConfigValidator.
func (c *Config) Validate() error {
	if c.DatabaseURL == "" {
		return errors.New("database_url é obrigatória")
	}
	if c.CacheTTL < 0 || c.CacheTTL > time.Minute {
		return fmt.Errorf("cache_ttl precisa ficar entre 0 e 1m, veio %s", c.CacheTTL)
	}
	return nil
}

// NewFactory cria a fábrica da extensão.
func NewFactory() extension.Factory {
	return extension.NewFactory(Type, createDefaultConfig, create, component.StabilityLevelDevelopment)
}

func createDefaultConfig() component.Config {
	return &Config{CacheTTL: time.Minute}
}

func create(_ context.Context, set extension.Settings, cfg component.Config) (extension.Extension, error) {
	return &ext{cfg: cfg.(*Config), set: set}, nil
}

// ext liga o authenticator ao ciclo de vida do coletor: abre o pool do banco
// no Start e o fecha no Shutdown.
type ext struct {
	cfg  *Config
	set  extension.Settings
	pool *pgxpool.Pool
	auth *authenticator
}

var _ extensionauth.Server = (*ext)(nil)

func (e *ext) Start(ctx context.Context, _ component.Host) error {
	pool, err := pgxpool.New(ctx, e.cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("ingestkeyauth: database_url: %w", err)
	}
	e.pool = pool
	e.auth = newAuthenticator(pgStore{pool: pool}, e.cfg.CacheTTL, e.set.Logger)
	return nil
}

func (e *ext) Shutdown(context.Context) error {
	if e.pool != nil {
		e.pool.Close()
	}
	return nil
}

func (e *ext) Authenticate(ctx context.Context, headers map[string][]string) (context.Context, error) {
	return e.auth.Authenticate(ctx, headers)
}
