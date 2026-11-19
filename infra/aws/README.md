# Recuperação da EC2 de produção

A pilha CloudFormation anexa recuperação de host, aviso por e-mail e backup diário à EC2 que já executa Docker. Use a região da instância. Confirme que seu tipo e tenancy suportam recuperação automática, que o Docker inicia no boot e que todos os volumes de dados do Compose residem em EBS. O cofre retém os pontos diários por 30 dias. Alarmes, snapshots e restaurações podem gerar cobrança. [Requisitos da recuperação de EC2](https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/cloudwatch-recovery.html)

```sh
aws cloudformation deploy --template-file infra/aws/ec2-recovery.yaml \
  --stack-name stellytics-recovery --region "$AWS_REGION" \
  --capabilities CAPABILITY_IAM \
  --parameter-overrides InstanceId="$INSTANCE_ID" OperationsEmail="$OPS_EMAIL"
```

Confirme a assinatura do SNS recebida por e-mail. Após o primeiro backup, restaure o ponto em uma EC2 isolada, sem DNS de produção e sem saída SMTP. Confirme a inicialização dos contêineres, a leitura dos dois bancos e a ingestão de um evento de teste. Registre o horário do ponto restaurado, o último evento disponível e o tempo até o painel exibi-lo. O intervalo diário pode custar até um dia de eventos ou mais se um backup falhar.

Após um alarme de sistema, consulte o resultado da recuperação no AWS Health, confira os serviços com `docker compose --env-file .env.production -f infra/compose/prod-vps.yaml ps` e valide a ingestão web, o coletor OTLP e o painel externamente. O alarme cobre falha do host físico. Falhas de aplicação, perda de uma zona e recuperação do banco exigem medidas adicionais.
