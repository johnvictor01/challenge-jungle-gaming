# Arquitetura inicial

Este documento registra a primeira proposta para o modelo de dados. Ele é um ponto de partida para revisão; as migrations e a implementação ficam para a próxima etapa, depois de validar as decisões de negócio.

## Escopo por etapas

1. Modelar dados e invariantes do domínio.
2. Definir e criar o schema PostgreSQL com migrations versionadas.
3. Cobrir domínio e persistência com testes.
4. Implementar casos de uso e API.
5. Integrar SQS e Keycloak, ampliar os testes de integração e fechar documentação e demonstração.

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

O primeiro desenho usa `SELECT ... FOR UPDATE` na linha da carteira. Isso serializa alterações da mesma carteira no PostgreSQL e deixa carteiras independentes avançarem em paralelo. Restrições e saldo condicional no banco continuam como defesa adicional. A criação de operações e a disputa de idempotência precisam tratar violações de unicidade como caminho normal de replay/conflito.

`LOSS` não altera saldo, versão nem ledger, mas conclui a operação e gera o evento de operação processada. `OPENING` cria carteira, operação interna e lançamento inicial atomicamente. O ledger não aceita edição ou exclusão por mecanismos do banco.

Uma operação de reversão sem referência disponível fica em `PENDING_REFERENCE`, com tentativas e prazo duráveis. A resolução da referência e a reversão bem-sucedida precisam ser serializadas no banco para impedir duas reversões incompatíveis.

## Decisões já alinhadas

- `player_id` identifica o usuário do Keycloak; não haverá tabela de jogadores nesta primeira modelagem.
- Um jogador pode ter uma carteira por moeda. A carteira tem seu próprio `wallet_id`, que será referenciado pelas operações.
- O histórico de idempotência financeira deve ser persistente e sem expiração planejada, conforme a exigência do challenge.

## Pontos para confirmar antes da primeira migration

1. **Provedor:** há cadastro/tabela de provedores ou o identificador do provedor autenticado fica diretamente nas transações?
2. **Resultado de replay:** qual saldo/resposta original deve ser salvo para que um replay receba o resultado da operação original?
3. **Reversões:** `REFUND` e `ROLLBACK` podem coexistir sobre a mesma operação? Quais combinações são válidas?
4. **Falhas permanentes:** em quais condições uma falha vira `FAILED`; falhas transitórias precisam continuar retomáveis.
5. **Inbox:** por quanto tempo guardar os IDs de mensagens SQS já concluídas? Isso é separado da retenção permanente da idempotência financeira.

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

Essa estrutura é intencionalmente apenas um mapa nesta etapa; os diretórios e componentes serão criados conforme cada parte for implementada.

## Pendências

Este projeto ainda não tem schema nem migrations. A próxima entrega deve fechar as seis decisões acima, desenhar as constraints/índices e então criar migrations pequenas e reversíveis por responsabilidade.
