package main

import (
	"context"
	"fmt"
	"math"
	"math/rand/v2"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// step é um degrau do perfil de carga: rate eventos por segundo durante dur.
type step struct {
	Rate float64
	Dur  time.Duration
}

func (s step) String() string { return fmt.Sprintf("%gx%s", s.Rate, s.Dur) }

func parseProfile(spec string) ([]step, error) {
	var out []step
	for _, part := range strings.Split(spec, ",") {
		r, d, ok := strings.Cut(strings.TrimSpace(part), "x")
		if !ok {
			return nil, fmt.Errorf("degrau %q: use taxaxduração, ex.: 11.6x2m", part)
		}
		rate, err := strconv.ParseFloat(r, 64)
		if err != nil || rate <= 0 {
			return nil, fmt.Errorf("degrau %q: taxa inválida", part)
		}
		dur, err := time.ParseDuration(d)
		if err != nil || dur <= 0 {
			return nil, fmt.Errorf("degrau %q: duração inválida", part)
		}
		out = append(out, step{rate, dur})
	}
	return out, nil
}

// recorder guarda latências e códigos por degrau e operação.
type recorder struct {
	mu   sync.Mutex
	data map[string]*opStats // "degrau/op"
}

type opStats struct {
	Step, Op string
	lats     []time.Duration
	Codes    map[string]int
}

func newRecorder() *recorder { return &recorder{data: map[string]*opStats{}} }

// add registra uma operação concluída. code é o status HTTP ou "err".
func (r *recorder) add(stepIdx int, op string, d time.Duration, code string) {
	key := fmt.Sprintf("%d/%s", stepIdx, op)
	r.mu.Lock()
	defer r.mu.Unlock()
	s := r.data[key]
	if s == nil {
		s = &opStats{Step: strconv.Itoa(stepIdx), Op: op, Codes: map[string]int{}}
		r.data[key] = s
	}
	s.Codes[code]++
	if code != "err" {
		s.lats = append(s.lats, d)
	}
}

func (s *opStats) quantile(q float64) time.Duration {
	if len(s.lats) == 0 {
		return 0
	}
	i := int(math.Ceil(q*float64(len(s.lats)))) - 1
	return s.lats[max(i, 0)]
}

func (s *opStats) total() int {
	n := 0
	for _, c := range s.Codes {
		n += c
	}
	return n
}

// report imprime as tabelas por degrau e operação.
func (r *recorder) report(steps []step) {
	r.mu.Lock()
	defer r.mu.Unlock()
	keys := make([]string, 0, len(r.data))
	for k := range r.data {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	fmt.Printf("\n%-22s %-10s %8s %9s %9s %9s %9s  %s\n", "degrau", "operação", "n", "p50", "p95", "p99", "max", "códigos")
	for _, k := range keys {
		s := r.data[k]
		slices.Sort(s.lats)
		idx, _ := strconv.Atoi(s.Step)
		codes := make([]string, 0, len(s.Codes))
		for c, n := range s.Codes {
			codes = append(codes, fmt.Sprintf("%s=%d", c, n))
		}
		slices.Sort(codes)
		var mx time.Duration
		if n := len(s.lats); n > 0 {
			mx = s.lats[n-1]
		}
		fmt.Printf("%-22s %-10s %8d %9s %9s %9s %9s  %s\n", steps[idx], s.Op, s.total(),
			s.quantile(.5).Round(10*time.Microsecond), s.quantile(.95).Round(10*time.Microsecond),
			s.quantile(.99).Round(10*time.Microsecond), mx.Round(10*time.Microsecond), strings.Join(codes, " "))
	}
}

// drive dispara fire em chegada de Poisson, degrau por degrau, sem esperar as
// respostas. maxInflight limita as goroutines; chegadas que passam disso são
// contadas em saturated (o gerador, não o servidor, é o gargalo). Devolve o
// total disparado e os instantes de início de cada degrau.
func drive(ctx context.Context, steps []step, maxInflight int, onStep func(i int, s step), fire func(ctx context.Context, stepIdx int)) (fired, saturated int64, starts []time.Time) {
	sem := make(chan struct{}, maxInflight)
	var wg sync.WaitGroup
	var nFired, nSat atomic.Int64
	for i, s := range steps {
		start := time.Now()
		starts = append(starts, start)
		if onStep != nil {
			onStep(i, s)
		}
		end := start.Add(s.Dur)
		next := start
		for {
			next = next.Add(time.Duration(rand.ExpFloat64() / s.Rate * float64(time.Second)))
			if next.After(end) || ctx.Err() != nil {
				break
			}
			if d := time.Until(next); d > 0 {
				time.Sleep(d)
			}
			select {
			case sem <- struct{}{}:
				nFired.Add(1)
				wg.Add(1)
				go func() {
					defer wg.Done()
					defer func() { <-sem }()
					fire(ctx, i)
				}()
			default:
				nSat.Add(1)
			}
		}
	}
	wg.Wait()
	return nFired.Load(), nSat.Load(), starts
}
