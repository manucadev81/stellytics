# 0003. Isolamento entre tenants é responsabilidade do portal

- **Status:** Aceito
- **Data:** 13/11/2026
- **Issue:** [#18](https://github.com/romulospl/notify/issues/18)
- **Altera em parte:** [ADR 0001](0001-ferramenta-base.md) (justificativa 3 e primeira consequência)

## Contexto

O ADR 0001 escolheu o Umami em parte porque ele já tem equipes e sites, e afirmou que "o isolamento parte de algo pronto". Previa também que o portal criaria usuário, equipe e site no Umami para cada tenant.

O ADR 0002 mudou o cenário:

- o portal é a única porta de entrada para tenants e operadores;
- o Umami é usado como motor headless e fica só na rede interna (issue [#62](https://github.com/romulospl/notify/issues/62));
- o portal lê as estatísticas com uma conta de serviço que enxerga todos os sites;
- as métricas de API ficam no banco `notify`, fora do Umami.

Nessas condições, o controle de acesso do Umami nunca é exercido por um tenant: quem chama o Umami é sempre o portal, com acesso total. O isolamento precisa de um dono claro, porque é critério de avaliação do RFP e exige testes automatizados (issue [#21](https://github.com/romulospl/notify/issues/21)).

## Decisão

O **portal é o único responsável pelo isolamento entre tenants**, tanto nos dados web (lidos do Umami) quanto nos dados de API (lidos do banco `notify`).

- O tenant é resolvido **sempre pela sessão**, no servidor. Nenhum valor enviado pelo cliente escolhe o tenant.
- Todo acesso a dados passa por uma **camada única**, que só aceita um contexto de acesso criado a partir da sessão. O ID de um site no Umami só é obtido por essa camada, depois de conferir que o site é do tenant.
- O portal cria no Umami **apenas sites**, todos pertencentes à conta de serviço. Não cria usuários nem equipes no Umami.
- O Umami não é acessível de fora da rede interna.

O contrato detalhado está no [spec do E2](../specs/e2-tenants-e-controle-de-acesso.md).

## Alternativas consideradas

| Alternativa | Prós | Contras |
|---|---|---|
| Um usuário e uma equipe no Umami por tenant, com o portal lendo com o token de cada tenant | Segunda barreira: o próprio Umami recusaria a leitura de outro tenant | O portal teria de guardar uma credencial do Umami por tenant; dois cadastros de usuário para manter em sincronia; não cobre as métricas de API |
| Row-Level Security do PostgreSQL no banco `notify` | Barreira no banco, independente do código do portal | Exige definir o tenant em cada conexão do pool; não cobre os dados do Umami. Pode ser somada depois, sem mudar esta decisão |
| Expor a interface do Umami aos tenants | Painel web pronto | Contraria o ADR 0002; não dá painel comparativo, nem de API, nem cadastro aberto |

## Consequências

- O argumento do ADR 0001 de que o isolamento parte do modelo de equipes do Umami deixa de valer para a leitura. As demais razões da escolha do Umami continuam.
- Um erro no portal pode expor dados de outro tenant, sem segunda barreira. Por isso:
  - a camada única é imposta por regra de lint;
  - a suíte de isolamento (#21) roda no CI e cresce a cada painel novo;
  - o gateway e o coletor usam usuários de banco próprios, com permissão mínima.
- O cadastro fica mais simples: um site no Umami por site do tenant, sem sincronizar usuários.
- A conta de serviço do Umami passa a ser uma credencial crítica. Fica só no portal, em variável de ambiente.
- A issue [#58](https://github.com/romulospl/notify/issues/58) cria só sites no Umami.
