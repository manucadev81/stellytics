# Política de segurança

O stellytics recebe dados de uso de vários projetos (tenants). Vulnerabilidades que permitam ler dados de outro tenant, burlar a chave de ingestão, persistir dados pessoais (como o IP completo) ou inflar métricas são tratadas como prioridade.

## Como relatar uma vulnerabilidade

**Não abra issue pública** para vulnerabilidades.

Use o relato privado do GitHub: na aba **Security** do repositório, clique em **Report a vulnerability**. O relato fica visível só para os mantenedores até a correção ser publicada.

O relato privado foi ativado em 19/11/2026. Não publique detalhes de vulnerabilidades em issues públicas.

Inclua, se possível:

- o componente afetado (portal, script de rastreamento, gateway, coletor, infraestrutura);
- os passos para reproduzir e o impacto esperado;
- a versão (commit, versão do script `/s/vX.Y.Z/` ou tag de release).

## Prazos de resposta

| Etapa | Prazo |
|---|---|
| Confirmação de recebimento | até 3 dias úteis |
| Avaliação inicial e severidade | até 72 horas corridas; priorizar imediatamente relatos potencialmente críticos ou altos |
| Correção de severidade crítica | até 72 horas corridas |
| Correção de severidade alta | até 72 horas corridas |
| Correção de severidade média ou baixa | até 90 dias corridos |

Os prazos contam do recebimento do relato ou da identificação pelo projeto; as 72 horas incluem noites, fins de semana e feriados. A severidade segue o CVSS 3.1. A avaliação de relatos potencialmente críticos ou altos deve ocorrer com antecedência suficiente para cumprir o prazo de correção, que não reinicia após a classificação. O prazo de 24 horas citado no RFP permanece uma meta externa, diferente do compromisso de 72 horas. Se não houver correção de uma dependência disponível ou o prazo estiver em risco, aplicamos contenção, registramos o risco e comunicamos a pessoa que relatou; isso não transforma contenção em correção.

Correções de segurança críticas ou altas podem dispensar a espera de 14 dias da dependência quando o risco de esperar superar o de atualizar. A exceção é específica à dependência e à versão, registrada e revisada no PR, com os testes e a publicação assinada descritos no [runbook](docs/runbooks/vulnerabilidades.md). Depois da correção publicada, divulgamos um advisory no GitHub e damos crédito a quem relatou, se a pessoa quiser.

## Versões suportadas

| Componente | Suportado |
|---|---|
| Plataforma hospedada (`main`) | Sim, sempre a versão em produção |
| Script de rastreamento | A versão mais recente de cada versão maior. Versões publicadas são imutáveis: correções saem em uma versão nova, e os tenants afetados são avisados |
| Releases antigas do repositório | Não |

## Escopo

Fora do escopo: ataques que exigem acesso físico ou credenciais já comprometidas do tenant, negação de serviço por volume e relatos automáticos de scanners sem prova de impacto.
