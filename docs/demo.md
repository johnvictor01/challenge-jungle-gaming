# Roteiro da demonstração local

Este roteiro permite apresentar o serviço e gravar a demonstração do challenge. Use somente as credenciais locais do `.env.example`.

## 1. Preparar os serviços

Na raiz do repositório:

```sh
cp .env.example .env
docker compose -f deploy/postgres.compose.yaml up -d postgres
docker compose -f deploy/postgres.compose.yaml --profile tools run --rm migrate
docker compose -f deploy/keycloak.compose.yaml up -d
docker compose -f deploy/sqs.compose.yaml up -d
```

Carregue as variáveis e inicie a API:

```sh
set -a
. ./.env
set +a
go run ./cmd/api
```

Em outro terminal, verifique readiness e métricas:

```sh
curl -i http://localhost:8080/health/live
curl -i http://localhost:8080/health/ready
curl -sS http://localhost:8080/metrics
```

Readiness só responde `200` quando PostgreSQL, fila de entrada e fila de eventos estão acessíveis.

## 2. Abrir uma carteira com o client interno

Obtenha um token `client_credentials` para `internal-service` e abra uma carteira com saldo inicial:

```sh
INTERNAL_TOKEN=$(curl -sS -X POST "$OIDC_ISSUER_URL/protocol/openid-connect/token" \
  -H 'Content-Type: application/x-www-form-urlencoded' \
  -d "grant_type=client_credentials&client_id=internal-service&client_secret=$TEST_INTERNAL_CLIENT_SECRET" | jq -r .access_token)

WALLET_RESPONSE=$(curl -sS -X POST http://localhost:8080/wallets \
  -H "Authorization: Bearer $INTERNAL_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"playerId":"demo-player-1","initialBalance":{"amount":"100.00","currency":"BRL"}}')
WALLET_ID=$(printf '%s' "$WALLET_RESPONSE" | jq -r .id)
```

Explique que a abertura positiva grava a carteira, a operação `OPENING`, o primeiro lançamento do ledger e os eventos outbox na mesma transação.

## 3. Processar e repetir uma aposta

Obtenha um token de `provider-a`. Envie uma aposta e repita exatamente os mesmos dados e a mesma chave:

```sh
PROVIDER_TOKEN=$(curl -sS -X POST "$OIDC_ISSUER_URL/protocol/openid-connect/token" \
  -H 'Content-Type: application/x-www-form-urlencoded' \
  -d "grant_type=client_credentials&client_id=provider-a&client_secret=$TEST_PROVIDER_CLIENT_SECRET" | jq -r .access_token)

BET_ID="demo-bet-$(date +%s)"
BET_BODY=$(printf '{"externalTransactionId":"%s","playerId":"demo-player-1","walletId":"%s","roundId":"demo-round","gameId":"demo-game","kind":"BET","money":{"amount":"25.00","currency":"BRL"}}' "$BET_ID" "$WALLET_ID")
curl -sS -X POST http://localhost:8080/wagering/transactions \
  -H "Authorization: Bearer $PROVIDER_TOKEN" \
  -H "Idempotency-Key: provider-a:$BET_ID" \
  -H 'Content-Type: application/json' -d "$BET_BODY"
curl -sS -X POST http://localhost:8080/wagering/transactions \
  -H "Authorization: Bearer $PROVIDER_TOKEN" \
  -H "Idempotency-Key: provider-a:$BET_ID" \
  -H 'Content-Type: application/json' -d "$BET_BODY"
```

A segunda resposta deve informar replay idempotente e preservar o saldo observado pela primeira operação. Consulte a carteira, o ledger e a reconciliação:

```sh
curl -sS -H "Authorization: Bearer $INTERNAL_TOKEN" "http://localhost:8080/wallets/$WALLET_ID"
curl -sS -H "Authorization: Bearer $INTERNAL_TOKEN" "http://localhost:8080/wallets/$WALLET_ID/ledger"
curl -sS -X POST -H "Authorization: Bearer $INTERNAL_TOKEN" "http://localhost:8080/wallets/$WALLET_ID/reconciliation"
```

## 4. Apresentar os testes de falha e concorrência

Pare a API com `Ctrl+C` e rode as verificações automatizadas:

```sh
go test -race ./...
TEST_DATABASE_URL="$DATABASE_URL" go test -race ./internal/adapters/postgres -count=1
TEST_DATABASE_URL="$DATABASE_URL" \
TEST_SQS_ENDPOINT="$SQS_ENDPOINT" \
TEST_SQS_INPUT_QUEUE_URL="$SQS_INPUT_QUEUE_URL" \
TEST_SQS_QUEUE_URL="$SQS_QUEUE_URL" \
AWS_ACCESS_KEY_ID=test AWS_SECRET_ACCESS_KEY=test AWS_REGION=us-east-1 \
go test ./internal/platform -run TestDependenciesReadinessAgainstPostgresAndLocalStack -count=1
```

Nos testes PostgreSQL, destaque os 50 replays distribuídos em três processos, a disputa por saldo insuficiente, a concorrência de `REFUND`/`ROLLBACK`, retomada de referência após reinício e conferência do saldo pelo ledger. Os testes do adapter SQS cobrem redelivery, backoff, DLQ e interrupções da outbox.

## Sugestão de ordem para a gravação

1. Mostrar a arquitetura e apontar tabelas, migrations e limites entre domínio, aplicação e adapters.
2. Demonstrar Keycloak emitindo os tokens de serviço e o provider.
3. Abrir carteira, processar aposta, repetir a aposta e conferir ledger/reconciliação.
4. Mostrar `/health/ready` e `/metrics`.
5. Rodar testes de concorrência e recuperação, explicando o saldo final e a garantia de idempotência.
