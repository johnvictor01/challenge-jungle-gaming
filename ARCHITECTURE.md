# Arquitetura

Este documento descreve a implementação que existe hoje e suas decisões. A API HTTP, o domínio, a persistência PostgreSQL, a integração OIDC com Keycloak, os workers de retomada, o consumidor SQS com inbox e o publisher da outbox fazem parte da aplicação em `cmd/api`, composta pelo Uber Fx.

## Componentes

| Componente | Responsabilidade |
| --- | --- |
| `internal/domain` | Valores monetários, carteiras, operações, ledger e invariantes puras |
| `internal/application` | Casos de uso, portas de persistência, hashing idempotente e despacho da outbox |
| `internal/adapters/http` | Contratos HTTP, autenticação, autorização e respostas |
| `internal/adapters/postgres` | Repositórios, transações SQL e migrations |
| `internal/adapters/sqs` | Consumo com inbox, publicação de eventos e integração AWS SDK |
| `internal/platform` | Configuração, workers, health/readiness e ciclo de vida |
| `cmd/api` | Grafo Fx e inicialização do processo |

Os detalhes de rotas e payloads estão em [docs/api-http-oidc.md](docs/api-http-oidc.md); o fluxo da inbox/outbox está em [docs/implementacao/sqs-outbox.md](docs/implementacao/sqs-outbox.md).

## Persistência e dinheiro

PostgreSQL é a autoridade para saldo, idempotência, inbox, ledger e outbox. O adapter usa `pgx` com SQL explícito. Valores são `int64` em unidades mínimas e são persistidos em `BIGINT`, junto de uma moeda ISO 4217 em maiúsculas. A API recebe strings decimais com duas casas; não usa ponto flutuante. O parsing e a aritmética detectam overflow.

As tabelas são `wallets`, `wager_transactions`, `wallet_ledger_entries`, `inbox_messages` e `outbox_events`. `player_id` é o `sub` estável do Keycloak: não existe cadastro de jogador local. Uma carteira tem ID próprio; `(player_id, currency)` é único. Assim, uma pessoa pode ter carteiras BRL e USD separadas.

Cada movimento executa numa transação SQL: bloqueia a carteira com `SELECT ... FOR UPDATE`, confere as regras, altera saldo/versão quando necessário, grava a operação e o ledger e insere os eventos de outbox. O adapter usa isolamento serializável, retry limitado de erros de serialização/deadlock e atualização condicionada pela versão como proteção adicional. As constraints do banco continuam responsáveis por unicidade, saldo não negativo e append-only do ledger. Carteiras diferentes não dependem de um lock global.

Saldo inicial positivo cria `OPENING`, lançamento de crédito e os dois eventos financeiros no mesmo commit. Saldo inicial zero cria apenas a carteira. `LOSS` conclui a operação e gera `WagerTransactionProcessed`, sem alterar saldo, versão ou ledger.

## Idempotência, referências e reversões

O hash SHA-256 é calculado de JSON canônico dos campos de negócio normalizados. Chave idempotente e metadados de transporte são excluídos, por isso HTTP e SQS calculam o mesmo hash. `(provider_id, idempotency_key)` e `(provider_id, external_transaction_id)` são únicos e persistidos sem expiração planejada. Replay devolve o saldo do processamento original; conteúdo diferente para a mesma chave resulta em conflito.

Na entrada SQS, `(consumer_name, message_id)` e o hash do envelope ficam na inbox. Inbox, operação e efeitos financeiros compartilham o commit SQL. A mensagem só é removida depois do commit. A deduplicação financeira da operação também funciona quando o mesmo comando entra por HTTP e SQS, independentemente de os message IDs serem diferentes.

`REFUND` e `ROLLBACK` exigem referência externa resolvida pelo provedor. Enquanto não existir, a operação fica em `PENDING_REFERENCE`; o worker persiste tentativas e o próximo instante de retry. O limite é dez tentativas, após as quais a operação vira `REJECTED` com `REFERENCE_NOT_FOUND`. Referência rejeitada ou falha também não pode ser revertida. A implementação permite no máximo uma reversão bem-sucedida por referência, considerando conjuntamente `REFUND` e `ROLLBACK`; uma reversão que exceda o saldo é rejeitada com `REVERSAL_INSUFFICIENT_FUNDS`. Essas decisões evitam devolver duas vezes o mesmo valor.

Erros de negócio produzem `REJECTED`; erros transitórios de infraestrutura fazem rollback para que a entrega possa ser repetida. O estado `FAILED` e sua transição de domínio existem, mas o worker ainda não classifica falhas permanentes de infraestrutura: falhas de publicação da outbox seguem em retry com backoff persistente. A política definitiva de falha permanente é uma limitação conhecida.

## Inbox, outbox e SQS

O consumidor usa a fila FIFO `wager-transactions.fifo`, timeout de visibilidade de 60 segundos e DLQ após cinco recebimentos. Erros transitórios ajustam a visibilidade com backoff exponencial entre 1 e 60 segundos. O `walletId` é `MessageGroupId`; o identificador estável da mensagem é `MessageDeduplicationId`. A janela FIFO é otimização, não a garantia de idempotência.

O publisher reivindica eventos confirmados com lease e `FOR UPDATE SKIP LOCKED`. Envia fora da transação; marca como publicado depois. Se houver queda após o envio e antes da confirmação, republica o mesmo `eventId` (at-least-once). Retentativas usam backoff persistido. Os eventos têm tipo, versão, correlação, causa, instante UTC e dados tipados; `WalletBalanceChanged` registra antes/depois e versão da carteira.

Credenciais e políticas AWS/LocalStack protegem acesso às filas. A identidade de um produtor SQS é a credencial/role AWS autorizada pelo broker, não o `provider_id` do OIDC. O consumidor valida o contrato e aplica as mesmas regras financeiras; em produção, permissões devem restringir quem publica na fila de entrada e quem lê/publica em cada fila.

## Identidade e autorização

Keycloak é o provedor OAuth 2.0/OIDC. A aplicação valida descoberta, emissor, assinatura, audiência e expiração do token. Os papéis de realm autorizam as rotas; `provider_id` vem do claim assinado do client e define o escopo das transações. Um valor de provedor no corpo não substitui a identidade autenticada. Provedores não consultam operações alheias; endpoints de carteira exigem o papel interno.

## Uber Fx e shutdown

Fx constrói configuração, pool PostgreSQL, autenticação OIDC, SDK SQS, repositórios, casos de uso, handler e workers. Os workers e o servidor são registrados no `fx.Lifecycle`. No encerramento, o servidor para de aceitar novas conexões; os workers recebem cancelamento e o Fx espera sua saída dentro do prazo; o pool PostgreSQL é fechado depois dos componentes que o usam. `TestApplicationStartsAndStopsWorkers` exercita start/stop contra PostgreSQL, Keycloak e LocalStack quando as variáveis de integração estão disponíveis.

## Logs e métricas

O processo configura `slog` com JSON. Os caminhos HTTP e SQS registram conclusão e falha com IDs de correlação/mensagem, transação quando conhecida, carteira e provedor, sem incluir credenciais ou payload financeiro. `/metrics` expõe contadores de resultado, duplicatas, conflitos, retries, redrive, publicação, latência e divergências de reconciliação. `/health/ready` consulta PostgreSQL e as filas SQS configuradas.

## Limitações atuais

- Os structs de domínio ainda expõem campos públicos. Métodos de transição preservam as invariantes quando usados, mas o compilador não impede escrita direta em `Money`, `Wallet`, `WagerTransaction` ou `WalletLedgerEntry`; portanto, o encapsulamento e a imutabilidade pedidos na seção 6 do README não estão completos.
- Falhas permanentes de infraestrutura ainda não são classificadas para gravar `FAILED`; o caminho automático atual retenta e usa DLQ para mensagens de entrada.
- O repositório entrega código, migrations, Compose por dependência, testes e documentação. A gravação e entrega do vídeo de demonstração é uma etapa manual do autor, descrita em [docs/demo.md](docs/demo.md).

O mapeamento requisito por requisito, incluindo evidências e trabalho restante, está em [docs/AUDITORIA_README.md](docs/AUDITORIA_README.md). O progresso de testes está em [docs/TODO_TESTES.md](docs/TODO_TESTES.md).
