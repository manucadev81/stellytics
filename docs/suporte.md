# Processo de suporte

Referência: [issue #52](https://github.com/romulospl/stellytics/issues/52). Dione e Rômulo, criadores do repositório, são responsáveis pelo suporte. O prazo assumido para atendimento e resolução é de até **10 dias úteis**. A primeira resposta em 5 dias úteis é uma meta de acompanhamento; o horário de atendimento ainda precisa ser definido.

## Entrada e triagem

- Para dúvidas e falhas sem informação sensível, usar as [issues do projeto](https://github.com/romulospl/stellytics/issues), com os modelos existentes. Não publicar credenciais, dados pessoais nem detalhes de vulnerabilidades.
- Vulnerabilidades seguem os prazos próprios de [SECURITY.md](../SECURITY.md), não este fluxo. O canal privado do GitHub estava desativado na última verificação; um administrador precisa ativá-lo antes de ele poder ser usado.
- Na triagem, registrar link, solicitante, data e hora de abertura em UTC, responsável, prioridade, descrição, próximo passo e prazo. Reconhecer o pedido e pedir informações ausentes sem divulgar dados sensíveis.

## Medição proposta

Primeira resposta é uma resposta humana que reconhece o pedido e informa o próximo passo; uma mensagem automática não conta. Resolução é a confirmação da correção ou uma resposta fundamentada que conclui o pedido, comunicada ao solicitante. Fechar uma issue sem comunicar o resultado não conta como resolução.

Os relógios começam no recebimento. Para contagem reproduzível, considerar o primeiro dia útil completo após o recebimento como dia 1. Usar o fuso `America/Los_Angeles`, inclusive suas mudanças de horário de verão, e excluir sábados, domingos e feriados federais dos Estados Unidos observados na data correspondente. Registrar o início e fim de qualquer espera por informações do solicitante ou por terceiro. Não descontar essas esperas dos indicadores até que uma regra de pausa seja aprovada e publicada. Reaberturas mantêm o histórico e recebem novo acompanhamento; não apagam o tempo da resolução anterior.

Revisar a fila em cada dia de atendimento, identificar prazos em risco e registrar bloqueios e comunicação. No relatório mensal, informar quantidade de chamados recebidos e resolvidos, tempos por chamado ou distribuição, metas cumpridas e pendências. Não publicar informação privada. O modelo de relatório está em [relatorios/modelo-sla.md](relatorios/modelo-sla.md).
