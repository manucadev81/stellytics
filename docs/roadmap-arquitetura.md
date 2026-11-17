# Roadmap de riscos arquiteturais

Este roadmap organiza os riscos identificados na revisão da arquitetura em relação ao RFP. Ele complementa o backlog existente; não substitui as issues já abertas. O status deve ser conferido no GitHub antes de criar issues, pois a consulta à API não estava disponível em 16/11/2026.

## Ordem sugerida

### P0 — Antes de operar com tenants reais

| Tema | Issue existente | Próximo passo | Critério de conclusão |
|---|---|---|---|
| Privacidade, retenção e apagamento | #40 documenta a situação; o andamento registra que não há issues de implementação | Abrir issue(s) para retenção de eventos Umami, exclusão de dados de projeto e conta, e tratamento de pedidos de titulares | Política e prazos documentados; exclusão executável e auditável; dados removidos também deixam de reaparecer após restauração de backup, dentro de prazo definido |
| Origem e repetição de eventos web | #41 documenta que `Origin` não é validado e a prova pode ser reutilizada durante sua validade | Abrir issue de decisão e mitigação; medir impacto de validar origem e tornar a prova de uso único ou limitar replay | A decisão explica o risco residual; testes demonstram que eventos de origens não autorizadas e replays fora da política escolhida são recusados |
| Isolamento de tenants | #21 implementa a suíte; ADR 0003 atribui o isolamento ao portal | Manter como controle contínuo: cada novo endpoint de leitura/escrita deve incluir testes de isolamento | CI cobre rotas e consultas novas; operadores e tenants têm escopos testados separadamente |
| Privacidade de dimensões de API | #32 normaliza padrões conhecidos; `http.route` personalizado pode conter dado pessoal | Abrir issue para política de validação/minimização de `http.route` e para casos não cobertos pela normalização | Exemplos de rotas com e-mail, nome, token ou segredo não persistem valores sensíveis; limites e comportamento ficam documentados |

### P1 — Antes de usar métricas em decisões do SCF

| Tema | Issue existente | Próximo passo | Critério de conclusão |
|---|---|---|---|
| Integridade e interpretação de métricas API | Nenhuma issue específica identificada | Abrir issue para documentar o modelo de confiança: a chave prova atribuição, não veracidade; definir sinais de anomalia sem alegar validação da atividade real | Dashboards e API explicam a procedência e limitações; limites e anomalias observáveis são definidos e testados |
| Definições comparáveis das métricas | #28/#29 e #37/#38 implementam dashboards | Abrir issue de glossário/metodologia para visitante único, sessão, bounce, janela temporal, timezone, p50/p95 e dados ausentes | Cada métrica tem definição, janela, limitações e tratamento de indisponibilidade documentados na UI e nos docs de operador |
| Auditoria de acesso global | #48 cobre chaves e API de leitura; ADR 0006 registra ausência de auditoria por leitura | Abrir issue para decidir e implementar auditoria proporcional de consultas com chave global | Operadores conseguem investigar uso indevido sem registrar segredo, payload sensível ou cardinalidade desnecessária; retenção do audit log definida |
| Abuso e cota de ingestão API | O andamento registra como pendentes limite por chave e réplica do coletor; verificar issues relacionadas antes de abrir duplicata | Completar limites por chave e alertas de uso anormal, respeitando tenants legítimos | Um tenant não consegue consumir recursos ilimitados; alertas e respostas a excesso estão documentados e testados |

### P2 — Robustez operacional e evolução

| Tema | Issue existente | Próximo passo | Critério de conclusão |
|---|---|---|---|
| Falhas de dependências e perda de eventos | #52 trata alta disponibilidade; ADR 0009 registra ausência de fila/buffer e perda possível | Usar #52 para decidir comportamento por dependência; abrir issue separada apenas se a decisão exigir fila durável | Para cada falha, há comportamento explícito (recusa, perda ou buffer), sinalização, recuperação e teste de exercício |
| Partilha justa da capacidade web | #134 trata limite por site nos repasses ao Umami | Concluir a decisão e medir contenção entre tenants; evitar uma cota fixa que desperdice capacidade | Um único site não monopoliza a ingestão quando há disputa; testes multi-tenant mostram a política e suas métricas |
| Resiliência do acesso privilegiado | #52 e runbooks cobrem disponibilidade/operação, mas não substituem o desenho de acesso | Confirmar que documentação operacional define concessão, revogação e revisão periódica de acesso a VM/banco | Acesso privilegiado tem responsáveis, procedimento de revogação e trilha de auditoria verificável |

## Issues novas propostas

As seguintes issues não foram encontradas no mapeamento local e devem ser criadas no GitHub após conferir duplicatas:

1. **Privacidade: retenção e apagamento de dados web, contas e tenants** — separar escopo legal/política do trabalho de implementação, incluindo backups e audit log.
2. **Gateway web: validar origem e limitar replay da prova de JavaScript** — começar por uma decisão de ameaça/compatibilidade e implementar a mitigação aprovada.
3. **API: proteger dimensões de rota contra dados pessoais e segredos** — revisar `http.route` e normalização padrão.
4. **Confiança das métricas de API: documentar procedência e detectar anomalias** — deixar claro o limite entre atribuição por chave e veracidade da instrumentação.
5. **Métricas: publicar definições e regras de comparação** — glossário para dashboards, API e operadores.
6. **API de leitura SCF: auditar consultas com credencial global** — avaliar granularidade, retenção e minimização dos logs.

Não abrir issues novas para isolamento (#21), alta disponibilidade/falhas (#52), limite por site (#134), load test (#51), nem chave de leitura (#48): já há trabalho associado. Confirmar o estado atual dessas issues antes de planejar execução.

## Dependências e sequência

```text
Privacidade/erasure ─┬─> pilotos com dados reais ─> operação
Origem/replay ───────┤
Rotas sem PII ───────┘

Definições de métricas ─> confiança/uso pelo SCF ─> comparação entre projetos
Auditoria da API global ─────────────────────────> consumidores downstream

HA/falhas (#52) + limite por site (#134) + limites API ─> SLA e escala
```

O RFP pede adoção real, serviço por 12 meses e métricas utilizáveis pelo SCF. Por isso, P0 deve preceder a instrumentação ampla de pilotos; P1 deve estar concluído ou explicitamente declarado como limitação antes que os dashboards sejam usados para decisões de financiamento. P2 pode ser entregue em etapas, desde que o comportamento e o risco residual estejam declarados.
