# Ensaio de recuperação da AWS

Objetivo: verificar a recuperação de dados e serviços e registrar resultados para a [issue #52](https://github.com/romulospl/stellytics/issues/52). Este roteiro não é evidência de execução nem prova das metas de disponibilidade.

## Preparação

1. Identifique conta, região, instância, volumes, serviços, versão implantada, responsável e janela. Confira custos, limites, permissões e o plano de reversão. Confirme que todos os dados necessários estão incluídos no backup; snapshots de volumes de bancos em execução podem exigir procedimentos adicionais de consistência.
2. Confirme o recebimento dos alertas, a existência de um ponto de recuperação concluído e a localização de segredos necessários. Não inclua segredos no relatório.
3. Prepare ambiente isolado, sem DNS de produção, sem envio de e-mails e sem acesso de escrita a serviços de produção. Use dados e credenciais de teste. Combine o teste antes de causar indisponibilidade em produção.

## Execução

1. Registre horário UTC do início, horário do ponto de recuperação e último evento conhecido antes do ponto. Restaure o backup em ambiente isolado conforme [recuperação EC2](../../infra/aws/README.md) e a documentação AWS aplicável.
2. Inicie a pilha e registre o horário em que serviços e bancos ficam utilizáveis. Verifique integridade e leitura dos bancos, sem assumir que um snapshot bem-sucedido garante consistência lógica.
3. Envie um evento de teste pela ingestão web e outro pelo coletor de API. Confirme aceitação e persistência, e que os dados esperados aparecem no painel ou na consulta de leitura correspondente. Registre comandos sem credenciais e resultados.
4. Verifique alertas e recuperação do host quando houver um método seguro e autorizado para exercitá-los. Uma restauração isolada não comprova que a recuperação automática do host funcionou. Não simule falha em produção sem procedimento e autorização específicos.
5. Registre fim do ensaio, remova recursos temporários conforme política da equipe e abra ações para falhas encontradas.

## Registro de evidências

- Data, ambiente, executor, versão e referências: [ ]
- Cenário exercitado e limitações: [ ]
- Início, ponto restaurado, último evento disponível, serviços prontos e fim, em UTC: [ ]
- Perda de dados observada e tempo até recuperação: [ ]; explique como foram calculados. RPO/RTO previstos são estimativas até medição representativa.
- Resultado de bancos, ingestão web, ingestão API, painel e alertas: [aprovado/falhou/não testado, evidência]
- Falhas, responsável, prazo e link da correção: [ ]
