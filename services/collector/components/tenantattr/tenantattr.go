// Package tenantattr carimba o tenant e o serviço da chave de ingestão em cada
// recurso que entra no coletor (#30, spec do E5, seção 1).
//
// O valor vem de client.Info.Auth, preenchido pela extensão ingestkeyauth; o
// que o cliente mandar nos atributos de recurso com esses nomes é apagado
// antes. Por isso este processor precisa vir antes do batch, que perde o
// contexto do pedido. Dado sem autenticação nunca passa daqui.
package tenantattr

import (
	"context"
	"errors"

	"go.opentelemetry.io/collector/client"
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/consumer"
	"go.opentelemetry.io/collector/consumer/consumererror"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/plog"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.opentelemetry.io/collector/pdata/ptrace"
	"go.opentelemetry.io/collector/processor"
	"go.opentelemetry.io/collector/processor/processorhelper"

	"github.com/romulospl/stellytics/services/collector/components/ingestkeyauth"
)

// Atributos de recurso que o processor define. Os componentes seguintes (o
// exporter da #31) leem só estes.
const (
	AttrTenantID  = "stellytics.tenant.id"
	AttrServiceID = "stellytics.api_service.id"
)

// spoofable são atributos de recurso que o cliente poderia mandar para se
// passar por outro tenant ou serviço. Todos saem antes do carimbo.
var spoofable = []string{AttrTenantID, AttrServiceID, "tenant.id", "tenant_id"}

// Type é o nome do processor na configuração do coletor.
var Type = component.MustNewType("tenantattr")

// errNoAuth: o pipeline recebeu dado sem passar pela extensão de autenticação.
var errNoAuth = errors.New("tenantattr: pedido sem tenant autenticado; o receiver precisa usar a extensão ingestkeyauth")

// Config não tem campos.
type Config struct{}

// NewFactory cria a fábrica do processor.
func NewFactory() processor.Factory {
	return processor.NewFactory(Type, func() component.Config { return &Config{} },
		processor.WithTraces(createTraces, component.StabilityLevelDevelopment),
		processor.WithMetrics(createMetrics, component.StabilityLevelDevelopment),
		processor.WithLogs(createLogs, component.StabilityLevelDevelopment),
	)
}

var mutates = processorhelper.WithCapabilities(consumer.Capabilities{MutatesData: true})

func createTraces(ctx context.Context, set processor.Settings, cfg component.Config, next consumer.Traces) (processor.Traces, error) {
	return processorhelper.NewTraces(ctx, set, cfg, next, func(ctx context.Context, td ptrace.Traces) (ptrace.Traces, error) {
		rs := td.ResourceSpans()
		return td, stamp(ctx, rs.Len(), func(i int) pcommon.Resource { return rs.At(i).Resource() })
	}, mutates)
}

func createMetrics(ctx context.Context, set processor.Settings, cfg component.Config, next consumer.Metrics) (processor.Metrics, error) {
	return processorhelper.NewMetrics(ctx, set, cfg, next, func(ctx context.Context, md pmetric.Metrics) (pmetric.Metrics, error) {
		rm := md.ResourceMetrics()
		return md, stamp(ctx, rm.Len(), func(i int) pcommon.Resource { return rm.At(i).Resource() })
	}, mutates)
}

func createLogs(ctx context.Context, set processor.Settings, cfg component.Config, next consumer.Logs) (processor.Logs, error) {
	return processorhelper.NewLogs(ctx, set, cfg, next, func(ctx context.Context, ld plog.Logs) (plog.Logs, error) {
		rl := ld.ResourceLogs()
		return ld, stamp(ctx, rl.Len(), func(i int) pcommon.Resource { return rl.At(i).Resource() })
	}, mutates)
}

// stamp troca os atributos de tenant e serviço de cada recurso pelos da chave.
func stamp(ctx context.Context, n int, resource func(int) pcommon.Resource) error {
	tenantID, serviceID, ok := fromAuth(ctx)
	if !ok {
		return consumererror.NewPermanent(errNoAuth)
	}
	for i := 0; i < n; i++ {
		attrs := resource(i).Attributes()
		for _, k := range spoofable {
			attrs.Remove(k)
		}
		attrs.PutStr(AttrTenantID, tenantID)
		attrs.PutStr(AttrServiceID, serviceID)
	}
	return nil
}

func fromAuth(ctx context.Context) (tenantID, serviceID string, ok bool) {
	auth := client.FromContext(ctx).Auth
	if auth == nil {
		return "", "", false
	}
	tenantID, _ = auth.GetAttribute(ingestkeyauth.AttrTenantID).(string)
	serviceID, _ = auth.GetAttribute(ingestkeyauth.AttrServiceID).(string)
	return tenantID, serviceID, tenantID != "" && serviceID != ""
}
