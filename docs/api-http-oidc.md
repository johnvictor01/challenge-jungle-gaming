# API HTTP e Keycloak

## Como a autenticação funciona

A API aceita access tokens OAuth 2.0 bearer emitidos pelo realm `backend-challenge`. `go-oidc` valida a assinatura com o JWKS publicado pelo Keycloak, o `iss`, a audiência `wager-api`, e as datas de validade. A API não emite tokens nem guarda senhas.

O token do client credentials do provedor contém `azp=provider-a`, `provider_id=provider-a` e os papéis `wager:read` e `wager:write`. O `provider_id` vem de um mapper assinado no realm. A API usa esse claim como identidade autorizada; se o JSON também enviar `providerId`, o valor precisa corresponder ao claim. Assim, o cliente não pode escolher qual provedor representa.

Os papéis são papéis de realm do Keycloak:

| Papel | Permissão |
| --- | --- |
| `wallet:read` | Consultar carteira e ledger |
| `wallet:write` | Abrir carteira |
| `wallet:reconcile` | Comparar saldo armazenado com o ledger |
| `wager:write` | Enviar operação externa |
| `wager:read` | Consultar operações do próprio provedor |

O realm local em `deploy/keycloak/realm` provisiona um provedor e um client interno para desenvolvimento. Os secrets e senhas definidos nele são somente valores locais de demonstração; ambientes reais devem fornecer credenciais pelo gerenciador de segredos.

## Rotas

| Método e rota | Papel | Resposta |
| --- | --- | --- |
| `GET /health/live` | Pública | Processo ativo |
| `GET /health/ready` | Pública | PostgreSQL disponível |
| `POST /wallets` | `wallet:write` | `201` e carteira criada |
| `GET /wallets/{walletID}` | `wallet:read` | Carteira |
| `GET /wallets/{walletID}/ledger?cursor=&limit=50` | `wallet:read` | Página com cursor opaco |
| `POST /wallets/{walletID}/reconciliation` | `wallet:reconcile` | Saldo armazenado, saldo reconstruído e diferença |
| `POST /wagering/transactions` | `wager:write` | `200`, `202` pendente ou `422` rejeitada |
| `GET /wagering/transactions/{transactionID}` | `wager:read` | Operação do provedor autenticado |
| `GET /providers/{providerID}/wagering/transactions/{externalID}` | `wager:read` | Operação externa do provedor autenticado |

Tokens ausentes ou inválidos recebem `401`; papel ausente recebe `403`. A consulta de operação alheia retorna `404` para não confirmar que aquele registro existe. Entrada inválida recebe `400`; duplicidade ou conflito de idempotência recebe `409`; indisponibilidade transitória recebe `503`.

## Executar localmente

Na raiz do repositório:

```sh
cp .env.example .env
docker compose -f deploy/postgres.compose.yaml up -d postgres
docker compose -f deploy/postgres.compose.yaml --profile tools run --rm migrate
docker compose -f deploy/keycloak.compose.yaml up -d
set -a
. ./.env
set +a
go run ./cmd/api
```

O endpoint de descoberta local é `http://localhost:8180/realms/backend-challenge/.well-known/openid-configuration`. O Keycloak cria o client `provider-a` para operações de jogo e `internal-service` para abrir carteiras e consultar dados internos.

Obter token local para o provedor:

```sh
curl -sS -X POST http://localhost:8180/realms/backend-challenge/protocol/openid-connect/token \
  -H 'Content-Type: application/x-www-form-urlencoded' \
  -d 'grant_type=client_credentials&client_id=provider-a&client_secret=provider-a-local-secret'
```

Use o `access_token` retornado como `Authorization: Bearer <token>`. Não envie o token ou secrets em logs ou commits.

Os testes unitários rodam com `go test ./...`. Para os testes PostgreSQL, aplique as migrations e execute:

```sh
set -a
. ./.env
set +a
go test ./internal/adapters/postgres -count=1
```

Com PostgreSQL e Keycloak locais ativos e as variáveis `TEST_DATABASE_URL`, `TEST_OIDC_ISSUER_URL`, `TEST_PROVIDER_CLIENT_SECRET` e `TEST_INTERNAL_CLIENT_SECRET` carregadas, execute os fluxos reais:

```sh
go test ./internal/adapters/http ./internal/adapters/postgres -count=1
```

## Estado e limites desta entrega

A API consulta PostgreSQL e compartilha os casos de uso. Os testes de integração usam tokens Keycloak reais e PostgreSQL real. O consumidor SQS, o publisher da outbox, métricas e a readiness combinada com SQS ainda pertencem às fases seguintes.
