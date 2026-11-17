package main

import (
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMetricsReportsSlotAndFailures(t *testing.T) {
	tests := []struct {
		name   string
		sample sample
		err    error
		want   []string
		absent string
	}{
		{"replicando", sample{1, 1, 123, 456}, nil, []string{"stellytics_replication_scrape_success 1", "stellytics_replication_slot_exists 1", "stellytics_replication_streaming 1", "stellytics_replication_lag_bytes 123", "stellytics_replication_retained_wal_bytes 456"}, ""},
		{"conexão falhou", sample{}, errors.New("senha secreta"), []string{"stellytics_replication_scrape_success 0"}, "senha secreta"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rr := httptest.NewRecorder()
			metrics(func(context.Context) (sample, error) { return tc.sample, tc.err })(rr, httptest.NewRequest("GET", "/metrics", nil))
			for _, want := range tc.want {
				if !strings.Contains(rr.Body.String(), want) {
					t.Errorf("não contém %q: %s", want, rr.Body.String())
				}
			}
			if tc.absent != "" && strings.Contains(rr.Body.String(), tc.absent) {
				t.Error("erro sensível exposto")
			}
		})
	}
}
