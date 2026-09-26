# SQS, inbox e outbox

O publisher da outbox e o provisionamento local do SQS estão implementados. A entrada SQS com inbox transacional continua pendente. Os eventos saem do banco depois do commit que os criou; publicação repetida mantém o mesmo `eventId`.

## Entrada pela inbox

O consumidor deve validar envelope e payload, usar `data.idempotencyKey` e gerar o mesmo hash canônico do endpoint HTTP. A identidade durável da entrega é `(consumer_name, message_id)`. Inbox, operação, saldo, ledger e eventos correspondentes precisam ser confirmados no mesmo `UnitOfWork`. Só remover a mensagem SQS depois do commit. Rejeições de negócio persistidas são terminais; falhas transitórias devem permitir retry.

## Publicação pela outbox — implementado

O publisher reivindica eventos em lotes com `FOR UPDATE SKIP LOCKED` e lease de 30 segundos. O envio à rede acontece fora da transação SQL. Depois de publicar, marca o evento como publicado; se houver interrupção entre envio e marcação, ele será publicado de novo com o mesmo `eventId`. Falhas incrementam tentativas e agendam backoff exponencial de 1 a 60 segundos. Falhas permanentes e DLQ ainda não têm política própria; o dispatcher continuará tentando.

Cada grupo FIFO usa o `aggregateId` como `MessageGroupId`, para que eventos da mesma carteira mantenham ordem e carteiras diferentes avancem em paralelo. O `eventId` vira `MessageDeduplicationId`.

## Entrada pela inbox — pendente

O consumidor deve validar envelope e payload, usar `data.idempotencyKey` e gerar o mesmo hash canônico do endpoint HTTP. A identidade durável da entrega é `(consumer_name, message_id)`. Inbox, operação, saldo, ledger e eventos correspondentes precisam ser confirmados no mesmo `UnitOfWork`. Só remover a mensagem SQS depois do commit. Rejeições de negócio persistidas são terminais; falhas transitórias devem permitir retry.

## Encerramento e recuperação

Em `SIGTERM`, parar novas leituras e concluir ou liberar o trabalho em andamento. Testar queda depois do commit antes do delete da mensagem, queda depois da publicação antes da confirmação, retomada por outra instância e duas instâncias concorrentes.

## Subir o SQS local

```sh
docker compose -f deploy/sqs.compose.yaml up -d
```

O serviço `create-queues` cria a fila FIFO `wager-events.fifo`. Configure `SQS_ENDPOINT=http://localhost:4566` e use a URL `http://sqs.us-east-1.localhost.localstack.cloud:4566/000000000000/wager-events.fifo` no `SQS_QUEUE_URL`. As credenciais `test/test` são somente para o LocalStack.

## Pendências de implementação

- [x] Publisher SQS com LocalStack e fila FIFO provisionada localmente.
- [ ] Repositório inbox e integração no `UnitOfWork`.
- [x] Reivindicação concorrente, lease, backoff e confirmação da outbox.
- [ ] Política e configuração de DLQ/visibility timeout.
- [x] Provisionamento automático da fila local.
- [ ] Testes reais de reentrega, interrupção, recuperação e concorrência.
