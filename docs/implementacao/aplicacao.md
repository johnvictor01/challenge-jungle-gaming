# Camada de aplicação e domínio

## Responsabilidade

`internal/domain` contém as regras de dinheiro, carteira, operação e ledger. Não importa HTTP, Keycloak, PostgreSQL ou SQS.

`internal/application` coordena essas regras usando portas. O adapter escolhe como implementar a `UnitOfWork` e os repositórios. A operação financeira grava, no mesmo callback transacional, o estado da operação, o saldo, o ledger e os eventos de outbox.

## Casos de uso

- `OpenWalletService`: cria uma carteira; com saldo inicial positivo grava `OPENING`, crédito no ledger e dois eventos. Com zero, cria somente a carteira.
- `ProcessWagerService`: valida idempotência, resolve regras de aposta e movimenta a carteira conforme `BET`, `WIN`, `LOSS`, `REFUND` ou `ROLLBACK`.
- `ResolvePendingReferenceService`: tenta novamente operações em `PENDING_REFERENCE`, agenda backoff exponencial ou rejeita depois de dez tentativas padrão.
- `PendingReferenceWorker`: busca no PostgreSQL referências pendentes que já podem ser tentadas e chama o serviço de retomada; a busca usa apenas estado persistido, então a retomada funciona depois de reiniciar o processo.
- `OutboxDispatcher`: reivindica eventos confirmados no banco, publica pelo port SQS e confirma ou agenda nova tentativa com backoff persistido.
- `ProcessInboxWagerService`: calcula o mesmo hash de negócio usado por HTTP, registra o ID da mensagem e chama o processamento financeiro dentro da mesma transação SQL.
- `QueryService`: consulta carteira/operação, pagina ledger com cursor opaco e compara saldo com o ledger sem alterar dados.

## Idempotência e reversões

O hash SHA-256 é calculado de JSON determinístico com os campos de negócio; IDs internos, chave de idempotência e metadados de transporte ficam de fora. HTTP e SQS normalizam a entrada e chamam o mesmo cálculo.

Uma repetição com chave e hash iguais devolve estado e saldo resultantes persistidos. Reutilizar a chave com payload diferente ou reaplicar o mesmo ID externo resulta em conflito. Só uma reversão `REFUND` ou `ROLLBACK` pode permanecer pendente ou ser processada para a mesma referência; uma reversão rejeitada continua auditável e não impede uma nova tentativa.

## Testes

Os testes de domínio e aplicação ficam junto dos pacotes. Os doubles em memória verificam regras e rollback da abstração; testes de persistência não usam esses doubles e ficam em `internal/adapters/postgres`.
