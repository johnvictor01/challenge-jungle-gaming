# Migrations do PostgreSQL

As migrations usam o formato do [golang-migrate](https://github.com/golang-migrate/migrate): cada mudança tem um arquivo `.up.sql` para aplicar e um `.down.sql` para reverter.

## PostgreSQL local com Docker Compose

Suba o PostgreSQL local:

```sh
docker compose -f deploy/postgres.compose.yaml up -d postgres
```

Aplique todas as migrations:

```sh
docker compose -f deploy/postgres.compose.yaml run --rm migrate up
```

Reverta a migration mais recente:

```sh
docker compose -f deploy/postgres.compose.yaml run --rm migrate down 1
```

O Compose usa usuário, senha, banco e porta locais de exemplo. Sobrescreva `POSTGRES_USER`, `POSTGRES_PASSWORD`, `POSTGRES_DB` e `POSTGRES_PORT` no ambiente para escolher outros valores. A senha padrão é apenas para desenvolvimento local.

Com o CLI `migrate` instalado e `DATABASE_URL` apontando para o PostgreSQL local:

```sh
migrate -path migrations -database "$DATABASE_URL" up
migrate -path migrations -database "$DATABASE_URL" down 1
```

Para reverter todas as migrations:

```sh
migrate -path migrations -database "$DATABASE_URL" down
```

Ordem atual:

1. `000001`: carteiras.
2. `000002`: operações, índices de idempotência e regra de operação `OPENING`.
3. `000003`: ledger append-only.
4. `000004`: inbox de mensagens SQS.
5. `000005`: outbox de eventos.

Se a carteira for aberta com saldo inicial maior que zero, a aplicação deve inserir a carteira, a operação `OPENING`, o lançamento de crédito no ledger e os eventos correspondentes na mesma transação SQL. Conforme o README do challenge, saldo inicial zero não cria operação `OPENING`, lançamento no ledger nem eventos financeiros.

O banco impede mais de uma reversão (`REFUND` ou `ROLLBACK`) para a mesma operação de origem. Uma reversão resolvida precisa apontar para uma operação processada compatível, na mesma carteira e rodada, e usar o mesmo valor. A política permite reverter um `REFUND` com `ROLLBACK`, conforme o tipo de referência aceito pelo README.
