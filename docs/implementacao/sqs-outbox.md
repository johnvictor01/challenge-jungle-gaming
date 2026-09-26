# SQS, inbox e outbox

O consumidor SQS com inbox transacional, o publisher da outbox e o provisionamento local estão implementados. Eventos saem do banco depois do commit que os criou; publicação repetida mantém o mesmo `eventId`.

## Entrada pela inbox — implementado

O consumidor valida envelope e payload, usa `data.idempotencyKey` e calcula o mesmo hash canônico do endpoint HTTP. A identidade durável da entrega é `(consumer_name, message_id)`. A inserção da inbox, a operação, o saldo, o ledger e os eventos são confirmados na mesma `UnitOfWork`. Só remove a mensagem SQS depois do commit. Rejeições de negócio persistidas são terminais; falhas de processamento deixam a mensagem na fila e ajustam a visibilidade com backoff exponencial de 1 a 60 segundos.

## Publicação pela outbox — implementado

O publisher reivindica eventos em lotes com `FOR UPDATE SKIP LOCKED` e lease de 30 segundos. O envio à rede acontece fora da transação SQL. Depois de publicar, marca o evento como publicado; se houver interrupção entre envio e marcação, ele será publicado de novo com o mesmo `eventId`. Falhas incrementam tentativas e agendam backoff exponencial de 1 a 60 segundos. Após dez tentativas, o evento recebe estado terminal de falha e deixa de ser reivindicado; eventos posteriores do agregado podem avançar.

Cada grupo FIFO usa o `aggregateId` como `MessageGroupId`, para que eventos da mesma carteira mantenham ordem e carteiras diferentes avancem em paralelo. O `eventId` vira `MessageDeduplicationId`.

## Encerramento e recuperação

Em `SIGTERM`, o consumer cancela long polling e espera o lote em andamento terminar; trabalho não confirmado permanece no SQS para reentrega depois do visibility timeout. O worker de outbox também encerra com o contexto do Fx. Testes cobrem redelivery depois de falha no delete, retomada por um novo serviço e republicação com o mesmo ID após falha de confirmação.

O worker de referências pendentes também inicia e encerra pelo ciclo de vida do Fx. Ele busca no PostgreSQL operações cujo próximo retry venceu, então não depende de memória local para recuperar após reinício. `/health/ready` consulta PostgreSQL e as filas SQS de entrada e de eventos; `/metrics` inclui contadores de retry/redrive e atraso de publicação.

## Subir o SQS local

```sh
docker compose -f deploy/sqs.compose.yaml up -d
```

O serviço `create-queues` cria as filas `wager-events.fifo`, `wager-transactions.fifo` e `wager-transactions-dlq.fifo`, com redrive após cinco recebimentos e visibility timeout de 60 segundos na fila de entrada. Configure `SQS_ENDPOINT=http://localhost:4566`, `SQS_QUEUE_URL` para a fila de eventos e `SQS_INPUT_QUEUE_URL` para a fila de entrada. As credenciais `test/test` são somente para o LocalStack.

Para executar a integração local completa, copie `.env.example` para `.env`, inicie Postgres, Keycloak e LocalStack com seus Compose em `deploy/`, aplique as migrations, exporte as variáveis com `set -a; source .env; set +a` e rode `go run ./cmd/api`. A API inicia os workers de entrada e saída junto do servidor HTTP.

Com os containers ativos e `.env` exportado, execute os testes integrados:

```sh
TEST_DATABASE_URL="$DATABASE_URL" \
TEST_SQS_ENDPOINT="$SQS_ENDPOINT" \
TEST_SQS_INPUT_QUEUE_URL="$SQS_INPUT_QUEUE_URL" \
TEST_SQS_DLQ_URL="$TEST_SQS_DLQ_URL" \
AWS_ACCESS_KEY_ID=test AWS_SECRET_ACCESS_KEY=test AWS_REGION=us-east-1 \
go test -race ./internal/adapters/postgres ./internal/adapters/sqs -count=1
```

## Implementação

- [x] Publisher SQS com LocalStack e fila FIFO provisionada localmente.
- [x] Repositório inbox e integração no `UnitOfWork`.
- [x] Reivindicação concorrente, lease, backoff e confirmação da outbox.
- [x] Política local de DLQ e visibility timeout.
- [x] Provisionamento automático da fila local.
- [x] Limite terminal de dez tentativas para publicação da outbox.
- [x] Testes reais de reentrega, interrupção, recuperação, claims concorrentes e DLQ.
- [x] Falhas técnicas repetidas ao resolver referência são contadas de forma persistente; a operação termina em `FAILED` com `REFERENCE_RESOLUTION_FAILED` e evento outbox.
