# Auditoria do challenge

Revisão do [README.md](../README.md) contra o código, os testes e os documentos do repositório. “Atendido” significa que há implementação e evidência executável; “parcial” indica uma limitação concreta; “manual” identifica uma entrega que depende do autor.

| Requisito do README | Estado | Evidência / observação |
| --- | --- | --- |
| API HTTP e consumidor para as mesmas operações | Atendido | `internal/adapters/http`, `internal/adapters/sqs`; teste de integração cruza uma aposta entre HTTP e SQS. |
| Keycloak/OIDC, papel e escopo do provedor | Atendido | `internal/adapters/http/oidc.go`; testes de token real e autorização; `docs/api-http-oidc.md`. |
| PostgreSQL, migrations e invariantes | Atendido | `migrations/`, `internal/adapters/postgres`; testes de constraints, atomicidade e reversão. |
| Precisão monetária, overflow e ISO 4217 | Atendido | `domain.Money` usa unidades inteiras e valida moeda ISO; testes cobrem inválidos e overflow. |
| Carteira, ledger append-only e saldo concorrente | Atendido | Constraints e proteções SQL, transação por carteira e cenários com três processos independentes. |
| Idempotência persistente, replays e conflito de payload | Atendido | Hash canônico e chaves únicas no PostgreSQL; HTTP e SQS compartilham o caso de uso. |
| Referência tardia, retry e recuperação após reinício | Atendido | `PENDING_REFERENCE`, tentativas persistidas e worker; teste reinicia o serviço. |
| Inbox e outbox transacionais, retry e recuperação | Atendido | Inbox compartilha a transação financeira; publisher usa lease, backoff e `eventId` estável. |
| DLQ, visibilidade e redelivery SQS | Atendido | Configuração local com cinco recebimentos e 60 s; integração LocalStack testa redrive. |
| Uber Fx e lifecycle dos workers/recursos | Atendido | `cmd/api/main.go`; teste de start/stop com PostgreSQL, Keycloak e LocalStack reais. |
| Logs JSON com IDs e métricas/readiness | Atendido | Logs HTTP, SQS e outbox têm contexto; `/metrics`, `/health/live` e `/health/ready`. |
| Testes unitários, integração real e `-race` | Atendido | Comandos e dependências em `docs/demo.md`; testes reais cobrem os serviços locais. |
| Encapsulamento das entidades e imutabilidade de `Money` | Parcial | Métodos validam transições, mas `Money`, `Wallet`, `WagerTransaction` e `WalletLedgerEntry` ainda expõem campos públicos. O código consumidor pode alterá-los diretamente. |
| Classificação automática de falha permanente como `FAILED` | Parcial | O estado/transição existem, mas os workers ainda mantêm retry; não há regra de classificação permanente nem gravação automática de `FAILED`. A outbox segue retry persistente. |
| Permissões do broker em produção | Parcial | O LocalStack provisiona filas/redrive; autenticação local usa credenciais de teste. Política IAM de produção precisa ser aplicada no ambiente de deploy. |
| Gravar e entregar vídeo de demonstração | Manual | O roteiro está em `docs/demo.md`; gravação/apresentação depende do autor. |

## Pontos a terminar

1. Encapsular os campos das quatro estruturas de domínio com construtores, reidratação e getters, atualizando aplicação, adapters e testes. Esse trabalho muda muitas chamadas e deve ser tratado como uma etapa própria para manter compilação e invariantes durante a migração.
2. Definir quais erros de infraestrutura são permanentes e quais são transitórios; implementar a política escolhida para persistir `FAILED`, emitir o evento correspondente e encerrar retries quando aplicável.
3. Aplicar uma política IAM/credenciais equivalente no ambiente de produção e verificar os papéis reais de produtor/consumidor.
4. Gravar o vídeo seguindo o roteiro de demonstração.

Comandos de verificação recomendados estão em `docs/demo.md`. A execução precisa das variáveis de integração listadas em `.env.example` e das migrations aplicadas.
