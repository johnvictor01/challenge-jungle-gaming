# Auditoria do challenge

Revisão do [README.md](../README.md) contra o código, os testes e os documentos do repositório. “Atendido” indica implementação com evidência executável; “parcial” indica uma limitação técnica documentada.

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
| Encapsulamento das entidades e imutabilidade de `Money` | Atendido | Os campos das quatro entidades são privados; getters retornam valores/cópias e factories/reidratação validam os dados. |
| Classificação de falhas técnicas como `FAILED` | Atendido | Tentativas de referência e publicação da outbox são persistidas; o limite da outbox é dez e eventos terminais deixam de bloquear os eventos seguintes do agregado. |
| Controle de acesso ao broker por credenciais e policies | Parcial | A aplicação usa as credenciais AWS do ambiente e o LocalStack aceita as credenciais locais `test/test`, mas o Compose não aplica policies às filas nem testa acesso permitido/negado. Em produção, roles e policies precisam ser configuradas no ambiente de deploy. |

## Resumo

O fluxo funcional e os testes foram verificados com PostgreSQL, Keycloak e LocalStack locais. Permanece parcial a política de autorização do broker: o repositório não provisiona nem testa policies de SQS; as credenciais e permissões de produção precisam ser definidas no ambiente de deploy.

Comandos de verificação recomendados estão em `docs/demo.md`. A execução precisa das variáveis de integração listadas em `.env.example` e das migrations aplicadas.
