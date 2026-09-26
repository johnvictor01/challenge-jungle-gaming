# Arquitetura inicial

Este documento registra as decisões de arquitetura adotadas até a fase HTTP/OIDC. Domínio, aplicação, migrations, adapter PostgreSQL e API autenticada estão implementados; consumidor SQS e publisher da outbox ficam para a próxima fase.

## Escopo por etapas

1. [x] Modelar dados e invariantes do domínio.
2. [x] Definir o schema PostgreSQL com migrations versionadas.
3. [x] Cobrir domínio e casos de uso com testes unitários.
4. [x] Implementar os casos de uso de abertura, operação e retomada de referência.
5. [x] Implementar persistência PostgreSQL e testes de integração básicos.
6. [x] Implementar API HTTP, validação OIDC, autorização por papel e reconciliação.
7. [x] Implementar consumidor SQS, inbox, publisher outbox e testes de recuperação.

## Proposta de persistência

PostgreSQL será a autoridade para saldo, idempotência e estado do processamento. Dinheiro será persistido em unidades mínimas: `amount_minor BIGINT` mais `currency CHAR(3)`. No domínio, `Money` continuará carregando valor e moeda; entradas e saídas externas usarão decimal textual com duas casas. Essa escolha evita `float` e torna comparações exatas. O domínio precisa detectar overflow antes de persistir.

Identificadores serão UUIDs gerados pela aplicação, exceto `player_id`, que identifica o usuário autenticado pelo identificador estável do Keycloak. Timestamps serão `TIMESTAMPTZ` em UTC. Um jogador pode ter várias carteiras, mas apenas uma por moeda; operações em moedas diferentes usam carteiras distintas.

### Entidades e responsabilidades

| Tabela | Papel | Restrições centrais propostas |
| --- | --- | --- |
| `wallets` | Uma carteira de um jogador em uma moeda, com saldo, versão e timestamps | `UNIQUE (player_id, currency)`, saldo não negativo, versão inicial positiva |
| `wager_transactions` | Operações internas e externas, idempotência, estado e resultado original | unicidade por provedor/chave e provedor/ID externo; tipo, origem e estado limitados a valores conhecidos |
| `wallet_ledger_entries` | Auditoria append-only de cada alteração efetiva de saldo | `UNIQUE (wallet_id, transaction_id)`, valor positivo, snapshots coerentes e proteção contra `UPDATE`/`DELETE` |
| `inbox_messages` | Deduplicação durável das entregas SQS | `UNIQUE (consumer_name, message_id)`, hash para detectar reentrega divergente |
| `outbox_events` | Eventos a publicar após commit | UUID estável, payload imutável, estado/tentativas/próximo envio e índice de busca de pendências |

### Campos a considerar

- `wallets`: `id` próprio da carteira, `player_id` do Keycloak, `currency`, `balance_minor`, `version`, `created_at`, `updated_at`. Cada linha representa a associação entre um jogador e o saldo que mantém naquela moeda. Transações referenciam `wallets.id`; esse identificador não é o `player_id`.
- `wager_transactions`: `id`, `origin` (`INTERNAL`/`EXTERNAL`), `kind`, `status`, IDs de carteira/jogador/provedor, `external_transaction_id`, `idempotency_key`, `payload_hash`, `round_id`, `game_id`, valor/moeda, referências externa e interna, `failure_code`, resultado de saldo original, timestamps e dados de retomada.
- `wallet_ledger_entries`: `id`, `wallet_id`, `transaction_id`, direção, valor/moeda, `balance_before_minor`, `balance_after_minor`, `created_at`.
- `inbox_messages`: `consumer_name`, `message_id`, hash, `received_at`, `completed_at` e referência opcional à operação.
- `outbox_events`: `event_id`, agregado, tipo/versão, correlação/causa, snapshot JSON, ocorrência, tentativas, `next_attempt_at`, lease/claim e `published_at`.

## Invariantes e transação financeira

Uma operação financeira e seus efeitos devem compartilhar uma única transação SQL: obter a carteira com coordenação por linha, validar saldo e regras, atualizar saldo e versão, gravar ledger, fechar estado da operação e inserir eventos na outbox. Quando a origem é SQS, conclusão da inbox entra nesse mesmo commit. A mensagem só é removida após o commit.

Em outras palavras, ao aceitar uma aposta, o banco atualiza o saldo e registra a movimentação no ledger no mesmo commit. Se também houver um evento para publicar, o registro desse evento entra nesse commit pela outbox. Um worker publica o evento depois. A outbox não calcula nem aplica a aposta; ela evita perder a notificação após o saldo ter sido confirmado. Se o saldo for insuficiente, a operação é registrada como rejeitada e não há débito nem lançamento financeiro no ledger.

O adapter PostgreSQL usa `pgx` com transações `SERIALIZABLE`, `SELECT ... FOR UPDATE` na linha da carteira e até três tentativas em falhas de serialização ou deadlock. Isso serializa alterações da mesma carteira e deixa carteiras independentes avançarem em paralelo. Restrições e o `UPDATE` condicionado pela versão continuam como defesa adicional. A criação de operações e a disputa de idempotência tratam violações de unicidade como caminho normal de replay ou conflito.

`LOSS` não altera saldo, versão nem ledger, mas conclui a operação e gera o evento de operação processada. `OPENING` cria carteira, operação interna e lançamento inicial atomicamente. O ledger não aceita edição ou exclusão por mecanismos do banco.

Uma operação de reversão sem referência disponível fica em `PENDING_REFERENCE`. O worker lê pendências vencidas diretamente do PostgreSQL e o caso de uso de retomada grava a próxima tentativa com backoff exponencial; no limite padrão de dez tentativas, encerra com `REFERENCE_NOT_FOUND`. A resolução da referência e a reversão bem-sucedida são serializadas no banco para impedir duas reversões incompatíveis. O schema reserva a referência para reversões pendentes ou processadas; rejeitadas permanecem no histórico sem impedir nova tentativa.

## Decisões já alinhadas

- `player_id` identifica o usuário do Keycloak; não haverá tabela de jogadores nesta primeira modelagem.
- Um jogador pode ter uma carteira por moeda. A carteira tem seu próprio `wallet_id`, que será referenciado pelas operações.
- O histórico de idempotência financeira deve ser persistente e sem expiração planejada, conforme a exigência do challenge.

## Regras definidas nesta fase

1. **Provedor:** o identificador fica na transação e vem do claim assinado `provider_id` do token Keycloak. Um `providerId` enviado no corpo é conferido e nunca define a identidade.
2. **Replay:** a transação persistida guarda estado, código de falha e saldo resultante; a mesma chave e o mesmo hash retornam esse resultado sem reaplicar o saldo.
3. **Reversões:** uma referência aceita somente uma reversão processada entre `REFUND` e `ROLLBACK`. Uma nova tentativa é recusada com `REFERENCE_ALREADY_REVERSED`.
4. **Falhas:** regra de negócio produz `REJECTED`; erros transitórios fazem rollback para permitir retry. `FAILED` fica reservado ao worker quando uma falha permanente for classificada.
5. **Inbox:** a deduplicação financeira é permanente. A retenção operacional da inbox será definida junto do consumidor SQS, sem afetar o histórico financeiro.
6. **SQS:** a inbox usa `(consumer_name, message_id)` e é confirmada junto dos efeitos financeiros. O `messageId` do envelope é a identidade estável da entrega; `data.idempotencyKey` continua sendo a chave financeira comum com HTTP.
7. **Publicação:** cada mensagem de entrada só é removida depois do commit SQL. Falhas de processamento deixam a mensagem para reentrega e redrive após cinco recebimentos. O publisher usa leases no PostgreSQL; eventos do mesmo agregado são publicados em ordem e o `eventId` é mantido em retries.

## Organização inicial do código

```text
cmd/api/                 entrada da API e composição Fx
cmd/worker/              entrada dos workers e composição Fx
internal/domain/         Money, Wallet, WagerTransaction, erros e regras puras
internal/application/    casos de uso e portas
internal/adapters/http/  handlers e contratos HTTP
internal/adapters/postgres/ repositórios e transações SQL
internal/adapters/sqs/   consumidor e publisher
internal/platform/       configuração, logging e composição compartilhada
migrations/              migrations PostgreSQL versionadas
deploy/                  Docker Compose e configuração local de serviços
```

Os diretórios estão criados. A composição de dependências fica no Uber Fx em `cmd/api`; regras ficam em `internal/application`; os adaptadores ligam HTTP e PostgreSQL. `go-oidc` valida emissor, assinatura, audiência e validade do access token. Papéis de realm controlam rotas e o claim `provider_id` vincula o client autenticado ao provedor.

## Estado atual

As etapas previstas no `docs/TODO_TESTES.md` estão implementadas e verificadas. O roteiro de execução da demonstração está em `docs/demo.md`.
