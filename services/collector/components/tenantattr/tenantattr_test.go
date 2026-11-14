package tenantattr

import (
	"context"
	"testing"

	"go.opentelemetry.io/collector/consumer/consumererror"
	"go.opentelemetry.io/collector/consumer/consumertest"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.opentelemetry.io/collector/processor/processortest"
)

// Sem passar pela extensão de autenticação, nada segue no pipeline.
func TestRejectsUnauthenticated(t *testing.T) {
	sink := new(consumertest.MetricsSink)
	p, err := NewFactory().CreateMetrics(context.Background(), processortest.NewNopSettings(Type), &Config{}, sink)
	if err != nil {
		t.Fatal(err)
	}
	md := pmetric.NewMetrics()
	md.ResourceMetrics().AppendEmpty().Resource().Attributes().PutStr(AttrTenantID, "forjado")
	err = p.ConsumeMetrics(context.Background(), md)
	if !consumererror.IsPermanent(err) {
		t.Fatalf("err = %v, quero erro permanente", err)
	}
	if sink.DataPointCount() != 0 || len(sink.AllMetrics()) != 0 {
		t.Error("dado sem autenticação seguiu no pipeline")
	}
}
