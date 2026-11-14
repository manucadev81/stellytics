// Package httpmetrics transforma o OTLP do tenant em pontos de 1 minuto (#31).
//
// Contrato no spec do E5, seções 2 a 4. Só duas entradas viram métrica: spans
// de servidor HTTP e histogramas http.server.request.duration (ou o antigo
// http.server.duration, em ms). Tudo o mais é descartado. De cada requisição
// ficam só o tenant e o serviço (do recurso carimbado pelo tenantattr), a rota
// normalizada, o método, o status e a duração; nenhum outro atributo sai daqui.
package httpmetrics

import (
	"time"

	"github.com/romulospl/notify/services/collector/components/routenorm"
)

// NumBuckets é o tamanho de duration_buckets (spec, seção 4, D2).
const NumBuckets = 15

// Bounds são os limites superiores, em segundos, dos 14 primeiros buckets; o
// último conta o que passa de 10 s. Iguais à api_metric_bounds() do banco.
var Bounds = [NumBuckets - 1]float64{0.005, 0.01, 0.025, 0.05, 0.075, 0.1, 0.25, 0.5, 0.75, 1, 2.5, 5, 7.5, 10}

// Rotas especiais (spec, seção 3).
const (
	RouteOther   = "{other}"   // além do teto de rotas por serviço e dia (D4)
	RouteUnknown = "{unknown}" // nem http.route nem caminho
)

// MaxRoutesPerDay é o teto de rotas distintas por serviço e dia (D4).
const MaxRoutesPerDay = 500

// MethodOther substitui métodos fora da lista padrão do HTTP.
const MethodOther = "_OTHER"

var methods = map[string]bool{
	"GET": true, "HEAD": true, "POST": true, "PUT": true, "DELETE": true,
	"CONNECT": true, "OPTIONS": true, "TRACE": true, "PATCH": true,
}

// Key identifica uma linha de api_metric_1m.
type Key struct {
	Bucket    time.Time // início do minuto, em UTC
	TenantID  string
	ServiceID string
	Route     string
	Method    string
	Status    int16
}

// Point é o que se soma numa linha.
type Point struct {
	Count   int64
	Sum     float64 // segundos
	Buckets [NumBuckets]int64
}

// Batch junta os pontos de um lote por linha.
type Batch map[Key]*Point

func (b Batch) add(k Key, count int64, sum float64, buckets [NumBuckets]int64) {
	if count <= 0 {
		return
	}
	p := b[k]
	if p == nil {
		p = &Point{}
		b[k] = p
	}
	p.Count += count
	p.Sum += sum
	for i := range buckets {
		p.Buckets[i] += buckets[i]
	}
}

// BucketIndex devolve o bucket de uma duração em segundos.
func BucketIndex(seconds float64) int {
	for i, ub := range Bounds {
		if seconds <= ub {
			return i
		}
	}
	return NumBuckets - 1
}

func normalizeMethod(m string) string {
	if methods[m] {
		return m
	}
	return MethodOther
}

func route(httpRoute, path string) string {
	if httpRoute == "" && path == "" {
		return RouteUnknown
	}
	return routenorm.Route(httpRoute, path)
}

func minute(t time.Time) time.Time {
	return t.UTC().Truncate(time.Minute)
}
