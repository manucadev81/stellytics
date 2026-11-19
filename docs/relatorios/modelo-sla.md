# Modelo de relatório mensal de operação

Copie para um arquivo ou issue por mês. Substitua campos entre colchetes; use **não medido** para dados ausentes, nunca zero. Não publique dados pessoais, segredos ou detalhes de vulnerabilidades ainda privadas.

- Período: [início inclusivo, fim exclusivo, UTC]
- Responsável e data: [nome, data]
- Fonte, ambiente e versão da configuração: [monitor, URLs ou identificadores sem segredos, versão]
- Cobertura e lacunas de coleta: [por componente; intervalos sem dados são desconhecidos]

| Componente | Sucessos / amostras | Disponibilidade observada | Tempo indisponível estimado | Tempo desconhecido | Exclusões e evidência |
|---|---|---|---|---|---|
| Ingestão web | | | | | |
| Ingestão API | | | | | |
| Painéis | | | | | |

Registre os horários e motivos das exclusões; sem decisão aprovada, apresente também o valor bruto. A fração de amostras não mede precisamente a duração de cada queda: informe intervalo de sondagem, períodos desconhecidos e método de cálculo. Não extrapole lacunas como disponibilidade ou indisponibilidade.

### Consulta ao Prometheus

No Prometheus do monitor, avalie no instante UTC de início do mês seguinte, usando a duração do mês em dias (`28d`, `29d`, `30d` ou `31d`) e cada valor de `component`: `ingest-web`, `ingest-api` e `portal`. Consulte tanto a proporção como a contagem:

```promql
avg_over_time(probe_success{job="endpoints",component="ingest-web"}[31d])
count_over_time(probe_success{job="endpoints",component="ingest-web"}[31d])
```

Substitua `31d` e o componente conforme o período. Os intervalos PromQL são abertos no início e fechados no fim; observe amostras na fronteira do mês e registre a convenção usada. Identifique separadamente cada série se houver troca de alvo; não some médias sem ponderar suas contagens nem conte alvos sobrepostos duas vezes. Com a configuração de 15 segundos, compare a contagem com o número esperado de sondagens e investigue lacunas, mudanças de configuração e duplicações. Uma série ausente não é 100% nem 0%. A retenção de 45 dias precisa estar implantada antes da coleta; salve consultas, horário de avaliação e resultados como evidência. Estas consultas ainda precisam de validação no monitor real.

## Incidentes e segurança

| Incidente ou referência pública | Início e fim UTC | Impacto | Causa, correção e acompanhamento |
|---|---|---|---|
| | | | |

| Vulnerabilidade ou referência segura | Severidade | Recebimento ou descoberta | Correção publicada | Prazo decorrido | Situação |
|---|---|---|---|---|---|
| | | | | |

## Suporte

Chamados recebidos: [ ]; resolvidos: [ ]; pendentes: [ ]. Informe tempos de primeira resposta e resolução, quantos cumpriram as metas de 5 e 10 dias úteis, respectivamente, e os bloqueios relevantes. Identifique chamados sem resposta ou sem resolução como pendentes, não como cumprimento. A metodologia e o calendário usados devem estar explícitos.

## Ações e decisões

| Ação | Responsável | Prazo | Referência |
|---|---|---|---|
| | | | |
