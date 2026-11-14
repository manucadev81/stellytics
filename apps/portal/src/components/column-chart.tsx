"use client";

import { useState } from "react";

// Colunas de uma série por balde do período, usadas pelos painéis do tenant
// (#28, visualizações; #37, requisições). Uma série só, então sem legenda: o
// título do cartão diz o que é. Cor validada para os dois modos (skill dataviz,
// slot 1: #2a78d6 no claro, #3987e5 no escuro). O tooltip só complementa: a
// tabela logo abaixo do gráfico traz os mesmos valores.

export type ColumnPoint = {
  label: string; // eixo x
  longLabel: string; // tooltip e tabela
  value: number; // altura da coluna
  description: string; // texto para leitor de tela, com todos os valores
  // Linhas do tooltip; a primeira é a da coluna e leva a amostra da cor.
  tooltip: { value: string; unit: string }[];
};

const full = new Intl.NumberFormat("en");
const compact = new Intl.NumberFormat("en", { notation: "compact", maximumFractionDigits: 1 });

// Topo do eixo e passo "redondos" (1, 2 ou 5 vezes uma potência de 10).
export function niceScale(max: number): { top: number; step: number } {
  if (max <= 0) return { top: 4, step: 1 };
  const raw = max / 3;
  const pow = 10 ** Math.floor(Math.log10(raw));
  const step = [1, 2, 5, 10].map((m) => m * pow).find((s) => s >= raw) ?? 10 * pow;
  const roundedStep = Math.max(1, step);
  return { top: Math.ceil(max / roundedStep) * roundedStep, step: roundedStep };
}

// Só alguns rótulos no eixo x (início, meio e fim), para não colidirem.
export function axisLabelIndexes(count: number): number[] {
  const last = count - 1;
  return last > 1 ? [0, Math.floor(last / 2), last] : [0, last];
}

export function ColumnChart({ points, label }: { points: ColumnPoint[]; label: string }) {
  const [active, setActive] = useState<number | null>(null);
  const { top, step } = niceScale(Math.max(...points.map((p) => p.value)));
  const ticks = Array.from({ length: Math.round(top / step) + 1 }, (_, i) => i * step);
  const tickFormat = top >= 10_000 ? compact : full;
  const hovered = active === null ? null : points[active];
  const dense = points.length > 40;

  return (
    <div className="[--grid:#e4e4e7] [--series:#2a78d6] dark:[--grid:#27272a] dark:[--series:#3987e5]">
      <div className="relative flex h-52">
        {/* Eixo y: marcas redondas, alinhadas à grade. */}
        <div className="relative w-10 shrink-0 text-right text-xs tabular-nums text-zinc-500" aria-hidden>
          {ticks.map((t) => (
            <span
              key={t}
              className="absolute right-2 translate-y-1/2 leading-none"
              style={{ bottom: `${(t / top) * 100}%` }}
            >
              {tickFormat.format(t)}
            </span>
          ))}
        </div>
        <div className="relative flex-1">
          {ticks.map((t) => (
            <div
              key={t}
              className="absolute inset-x-0 h-px bg-[var(--grid)]"
              style={{ bottom: `${(t / top) * 100}%` }}
              aria-hidden
            />
          ))}
          <div
            role="list"
            aria-label={label}
            className={`absolute inset-0 flex items-end ${dense ? "gap-px" : "gap-0.5"}`}
            onPointerLeave={() => setActive(null)}
          >
            {points.map((p, i) => (
              <div
                key={i}
                role="listitem"
                tabIndex={0}
                aria-label={p.description}
                className="flex h-full min-w-0 flex-1 cursor-default items-end justify-center outline-none focus-visible:bg-zinc-500/10"
                onPointerEnter={() => setActive(i)}
                onFocus={() => setActive(i)}
                onBlur={() => setActive(null)}
              >
                {p.value > 0 && (
                  <div
                    className={`w-full max-w-6 rounded-t-[4px] bg-[var(--series)] transition-opacity ${
                      active === i ? "opacity-75" : ""
                    }`}
                    style={{ height: `${(p.value / top) * 100}%` }}
                  />
                )}
              </div>
            ))}
          </div>

          {hovered && (
            <div
              className="pointer-events-none absolute top-0 z-10 rounded-md border border-zinc-200 bg-white px-3 py-2 text-sm shadow-sm dark:border-zinc-700 dark:bg-zinc-900"
              style={
                active! < points.length / 2
                  ? { left: `${(active! / points.length) * 100}%` }
                  : { right: `${((points.length - active! - 1) / points.length) * 100}%` }
              }
            >
              <p className="text-xs text-zinc-500">{hovered.longLabel}</p>
              {hovered.tooltip.map((line, i) => (
                <p key={i} className={`flex items-center gap-2 whitespace-nowrap ${i === 0 ? "mt-1" : "mt-0.5 pl-5"}`}>
                  {i === 0 && <span className="inline-block h-0.5 w-3 rounded bg-[var(--series)]" aria-hidden />}
                  <span className="font-semibold">{line.value}</span>
                  <span className="text-zinc-500">{line.unit}</span>
                </p>
              ))}
            </div>
          )}
        </div>
      </div>
      <AxisLabels labels={points.map((p) => p.label)} />
    </div>
  );
}

// Eixo x. Com poucas colunas, cada rótulo fica centrado na sua; com muitas, as
// pontas encostam nas bordas para não sair do cartão.
export function AxisLabels({ labels, offsetClass = "ml-10" }: { labels: string[]; offsetClass?: string }) {
  return (
    <div className={`relative ${offsetClass} mt-1 h-4 text-xs text-zinc-500`} aria-hidden>
      {axisLabelIndexes(labels.length).map((i) => {
        const edge = labels.length > 12 && (i === 0 || i === labels.length - 1);
        const left = edge ? (i === 0 ? 0 : 100) : ((i + 0.5) / labels.length) * 100;
        const shift = edge ? (i === 0 ? "" : "-translate-x-full") : "-translate-x-1/2";
        return (
          <span key={i} className={`absolute whitespace-nowrap ${shift}`} style={{ left: `${left}%` }}>
            {labels[i]}
          </span>
        );
      })}
    </div>
  );
}
