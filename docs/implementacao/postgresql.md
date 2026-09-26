# Persistência PostgreSQL

## Delimitação da transação

`internal/adapters/postgres.Store` implementa `application.UnitOfWork`. Cada callback recebe repositórios associados ao mesmo `pgx.Tx`; se uma etapa ou o commit falhar, a transação é revertida. A configuração usa `SERIALIZABLE`, com até três tentativas para falha de serialização ou deadlock.

O repositório de carteira usa `SELECT ... FOR UPDATE` antes da operação. A gravação condiciona a versão esperada. Assim, duas operações sobre a mesma carteira são ordenadas pelo banco, enquanto carteiras distintas não compartilham lock de aplicação.

## Dinheiro e ledger

Dinheiro é persistido como `BIGINT` em unidades mínimas e código `CHAR(3)`. Repositório nenhum usa ponto flutuante. Ledger só expõe inserção na aplicação; trigger PostgreSQL impede `UPDATE`, `DELETE` e `TRUNCATE`.

## Migrations e execução

As migrations em `migrations/` são versionadas e têm arquivos de subida e reversão. Os comandos locais estão em [migrations/README.md](../../migrations/README.md). Testes de integração exigem `TEST_DATABASE_URL` e um PostgreSQL com as migrations aplicadas.

## Verificações atuais

Os testes em `schema_integration_test.go` cobrem abertura e aposta atômicas, saldo e versão, ledger, outbox, replay, reconciliação, paginação, constraints de unicidade, carteira única por jogador/moeda, isolamento de provedor, reversões concorrentes, 50 replays em três processos e recuperação de referência pendente após reinício.
