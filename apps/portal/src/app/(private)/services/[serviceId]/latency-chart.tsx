"use client";

import { useState } from "react";
import { AxisLabels, useRovingFocus } from "@/components/column-chart";
import { formatLatency } from "@/lib/format";

// Latência p50 e p95 por balde do período (#37). Duas séries: legenda acima e
// rótulo direto no fim de cada linha, para a cor nunca ser a única pista.
// Cores das posições 1 e 2 da paleta categórica da skill dataviz (azul e
// laranja), validadas para os dois modos. Balde sem requisição é um buraco na
// linha, não zero. O tooltip só complementa: a tabela abaixo tem os valores.

export type LatencyPoint = { label: string; longLabel: string; p50: number | null; p95: number | null };

const SERIES = [
  { key: "p50", name: "p50 (median)", color: "var(--p50)" },
  { key: "p95", name: "p95", color: "var(--p95)" },
] as const;

// Topo do eixo e passo "redondos" em segundos (1, 2 ou 5 vezes uma potência de 10).
function niceSeconds(max: number): { top: number; step: number } {
  if (max <= 0) return { top: 0.1, step: 0.025 };
  const raw = max / 3;
  const pow = 10 ** Math.floor(Math.log10(raw));
  const step = [1, 2, 2.5, 5, 10].map((m) => m * pow).find((s) => s >= raw) ?? 10 * pow;
  return { top: Math.ceil(max / step - 1e-9) * step, step };
}

// Trechos contínuos de uma série, como coordenadas do SVG (x no centro do balde).
function segments(values: (number | null)[], top: number): { x: number; y: number }[][] {
  const out: { x: number; y: number }[][] = [];
  let cur: { x: number; y: number }[] = [];
  values.forEach((v, i) => {
    if (v === null) {
      if (cur.length) out.push(cur);
      cur = [];
    } else cur.push({ x: i + 0.5, y: 100 - (Math.min(v, top) / top) * 100 });
  });
  if (cur.length) out.push(cur);
  return out;
}

function lastValue(values: (number | null)[]): number | null {
  for (let i = values.length - 1; i >= 0; i--) if (values[i] !== null) return values[i]!;
  return null;
}

export function LatencyChart({ points }: { points: LatencyPoint[] }) {
  const [active, setActive] = useState<number | null>(null);
  const n = points.length;
  const values = { p50: points.map((p) => p.p50), p95: points.map((p) => p.p95) };
  const { top, step } = niceSeconds(Math.max(0, ...values.p95.map((v) => v ?? 0), ...values.p50.map((v) => v ?? 0)));
  const ticks = Array.from({ length: Math.round(top / step) + 1 }, (_, i) => i * step);
  const hovered = active === null ? null : points[active];
  const roving = useRovingFocus(n, setActive);

  // Rótulos diretos à direita, na altura do último valor; afastados se colidirem.
  const [p50End, p95End] = SERIES.map((s) => {
    const v = lastValue(values[s.key]);
    return v === null ? null : 100 - (Math.min(v, top) / top) * 100;
  }) as [number | null, number | null];
  const ends: (number | null)[] = [p50End, p95End];
  if (p50End !== null && p95End !== null && Math.abs(p50End - p95End) < 9) {
    // p95 >= p50, então o rótulo do p95 fica acima.
    const mid = (p50End + p95End) / 2;
    ends[1] = Math.max(0, mid - 4.5);
    ends[0] = Math.min(100, mid + 4.5);
  }

  return (
    <div className="[--grid:#e4e4e7] [--p50:#2a78d6] [--p95:#eb6834] dark:[--grid:#27272a] dark:[--p50:#3987e5] dark:[--p95:#d95926]">
      <ul className="flex flex-wrap gap-4 text-xs text-zinc-500" aria-label="Legend">
        {SERIES.map((s) => (
          <li key={s.key} className="flex items-center gap-1.5">
            <span className="inline-block h-0.5 w-3 rounded" style={{ background: s.color }} aria-hidden />
            {s.name}
          </li>
        ))}
      </ul>
      <div className="relative mt-3 flex h-52">
        <div className="relative w-14 shrink-0 text-right text-xs tabular-nums text-zinc-500" aria-hidden>
          {ticks.map((t) => (
            <span key={t} className="absolute right-2 translate-y-1/2 leading-none" style={{ bottom: `${(t / top) * 100}%` }}>
              {t === 0 ? "0" : t >= 1 && Number.isInteger(t) ? `${t} s` : formatLatency(t)}
            </span>
          ))}
        </div>
        <div className="relative flex-1">
          {ticks.map((t) => (
            <div key={t} className="absolute inset-x-0 h-px bg-[var(--grid)]" style={{ bottom: `${(t / top) * 100}%` }} aria-hidden />
          ))}
          {active !== null && (
            <div
              className="absolute inset-y-0 w-px bg-zinc-400 dark:bg-zinc-500"
              style={{ left: `${((active + 0.5) / n) * 100}%` }}
              aria-hidden
            />
          )}
          <svg
            className="absolute inset-0 h-full w-full overflow-visible"
            viewBox={`0 0 ${n} 100`}
            preserveAspectRatio="none"
            aria-hidden
          >
            {SERIES.map((s) =>
              segments(values[s.key], top).map((seg, i) =>
                seg.length > 1 ? (
                  <polyline
                    key={`${s.key}-${i}`}
                    points={seg.map((p) => `${p.x},${p.y}`).join(" ")}
                    fill="none"
                    stroke={s.color}
                    strokeWidth={2}
                    strokeLinejoin="round"
                    strokeLinecap="round"
                    vectorEffect="non-scaling-stroke"
                  />
                ) : null,
              ),
            )}
          </svg>
          {/* Pontos: o balde em foco e os isolados (sem vizinho, a linha não aparece). */}
          {SERIES.map((s) =>
            values[s.key].map((v, i) => {
              const isolated = v !== null && values[s.key][i - 1] == null && values[s.key][i + 1] == null;
              if (v === null || (!isolated && active !== i)) return null;
              return (
                <span
                  key={`${s.key}-${i}`}
                  className="absolute size-2 -translate-x-1/2 translate-y-1/2 rounded-full ring-2 ring-white dark:ring-zinc-900"
                  style={{ left: `${((i + 0.5) / n) * 100}%`, bottom: `${(Math.min(v, top) / top) * 100}%`, background: s.color }}
                  aria-hidden
                />
              );
            }),
          )}
          <div
            role="list"
            aria-label="Latency per period. Use the arrow keys to move between periods."
            className="absolute inset-0 flex"
            onPointerLeave={() => setActive(null)}
            onKeyDown={roving.onKeyDown}
          >
            {points.map((p, i) => (
              <div
                key={i}
                role="listitem"
                {...roving.item(i)}
                aria-label={
                  p.p50 === null
                    ? `${p.longLabel}: no requests`
                    : `${p.longLabel}: p50 ${formatLatency(p.p50)}, p95 ${formatLatency(p.p95)}`
                }
                className="h-full min-w-0 flex-1 cursor-default outline-none focus-visible:bg-zinc-500/10"
                onPointerEnter={() => setActive(i)}
                onFocus={() => setActive(i)}
                onBlur={() => setActive(null)}
              />
            ))}
          </div>
          {hovered && (
            <div
              className="pointer-events-none absolute top-0 z-10 rounded-md border border-zinc-200 bg-white px-3 py-2 text-sm shadow-sm dark:border-zinc-700 dark:bg-zinc-900"
              style={active! < n / 2 ? { left: `${((active! + 1) / n) * 100}%` } : { right: `${((n - active!) / n) * 100}%` }}
            >
              <p className="text-xs text-zinc-500">{hovered.longLabel}</p>
              {hovered.p50 === null ? (
                <p className="mt-1 text-zinc-500">No requests</p>
              ) : (
                SERIES.map((s) => (
                  <p key={s.key} className="mt-1 flex items-center gap-2 whitespace-nowrap">
                    <span className="inline-block h-0.5 w-3 rounded" style={{ background: s.color }} aria-hidden />
                    <span className="font-semibold">{formatLatency(hovered[s.key])}</span>
                    <span className="text-zinc-500">{s.key}</span>
                  </p>
                ))
              )}
            </div>
          )}
        </div>
        <div className="relative w-9 shrink-0 text-xs text-zinc-500" aria-hidden>
          {SERIES.map((s, i) =>
            ends[i] === null ? null : (
              <span key={s.key} className="absolute left-1.5 -translate-y-1/2 leading-none" style={{ top: `${ends[i]}%` }}>
                {s.key}
              </span>
            ),
          )}
        </div>
      </div>
      <div className="mr-9">
        <AxisLabels labels={points.map((p) => p.label)} offsetClass="ml-14" />
      </div>
    </div>
  );
}
