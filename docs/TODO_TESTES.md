# Roteiro de implementação e testes

Este arquivo acompanha os requisitos do `README.md` e registra o que foi implementado e verificado. Domínio, aplicação, PostgreSQL, API HTTP, validação OIDC, workers de referência e outbox, consumidor SQS com inbox transacional, métricas e readiness estão implementados.

## 1. Domínio

- [x] `Money`: análise decimal exata, valores inválidos, overflow, operações aritméticas, comparação e serialização com moeda.
- [x] `Wallet`: criação, reidratação, saldo não negativo, moeda, versão, crédito, débito e overflow.
- [x] `WagerTransaction`: tipos, valores, referências e mudanças de estado permitidas.
- [x] `OPENING`: operação interna processada para uma carteira com saldo inicial positivo.
- [x] `WalletLedgerEntry`: direção, valor, moeda, saldos antes/depois, overflow, IDs e reidratação.
- [x] Cobrir referências tardias, `REFUND`/`ROLLBACK` concorrentes, rejeição por referência já revertida e nova tentativa após uma reversão rejeitada nos testes PostgreSQL.

## 2. Casos de uso

Os testes executáveis usam repositórios em memória para testar as regras e a atomicidade esperada da `UnitOfWork`. Eles não substituem os testes do adapter PostgreSQL.

- [x] `OpenWallet`: saldo positivo cria carteira, `OPENING`, ledger e dois eventos de outbox.
- [x] `OpenWallet`: saldo zero cria só a carteira.
- [x] `OpenWallet`: impede carteira duplicada por jogador e moeda e rejeita dados inválidos.
- [x] `OpenWallet`: falha ao gravar outbox e não deixa alterações parciais.
- [x] `ProcessWager`: aposta debitada, saldo insuficiente e `LOSS` sem alteração de saldo.
- [x] `ProcessWager`: replay idempotente não repete débito, ledger ou eventos.
- [x] `ProcessWager`: conflito quando a chave idempotente ou o ID externo é reutilizado incorretamente.
- [x] `ProcessWager`: referência ausente fica `PENDING_REFERENCE`; reembolso com referência válida é aplicado.
- [x] `ProcessWager`: rollback rejeitado por saldo insuficiente não altera saldo nem ledger.
- [x] `ProcessWager`: segunda reversão bem-sucedida da mesma referência é rejeitada com `REFERENCE_ALREADY_REVERSED`.
- [x] `ProcessWager`: erro ao gravar outbox reverte as gravações da `UnitOfWork` simulada.
- [x] `ResolvePendingReference`: tenta novamente em referência ausente, persiste a próxima tentativa e rejeita com `REFERENCE_NOT_FOUND` quando o limite termina.
- [x] `ResolvePendingReference`: retoma uma operação pendente quando a referência chega, aplicando saldo, ledger e eventos no mesmo fluxo.
- [x] Hash canônico determinístico compartilhado entre HTTP e SQS.
- [x] Testar duas apostas simultâneas de `80.00` sobre saldo de `100.00`; uma processa e a outra é rejeitada.
- [x] Com PostgreSQL, persistir uma referência pendente, encerrar o pool/serviço, iniciar uma nova instância, inserir a referência e retomar a operação com sucesso.

## 3. PostgreSQL e migrations

- [x] Implementar repositórios e `UnitOfWork` PostgreSQL com `SELECT ... FOR UPDATE`, isolamento serializável e controle otimista de versão.
- [x] Aplicar migrations em PostgreSQL real.
- [x] Testar atomicidade de saldo, operação, ledger e outbox no fluxo de aposta.
- [x] Testar que ledger não pode ser apagado.
- [x] Testar aplicação e reversão completa das migrations em banco descartável.
- [x] Testar diretamente constraints de carteira por jogador/moeda e unicidade de idempotência, além dos fluxos de reversão.
- [x] Testar publisher, retry, disputa real de claims e ordenação por agregado.
- [x] Simular parada e reinício do serviço durante referência pendente e confirmar que saldo e ledger continuam coerentes.

## 4. HTTP, Keycloak e autorização

- [x] Implementar rotas do README e composição Uber Fx.
- [x] Validar token OIDC por emissor, assinatura, audiência e validade.
- [x] Usar o claim assinado `provider_id` e bloquear tentativa de informar outro provedor.
- [x] Autorizar por papéis Keycloak e ocultar operações de outros provedores.
- [x] Implementar paginação do ledger, reconciliação e health checks.
- [x] Testar token ausente, papel ausente e spoofing de provedor.
- [x] Importar o realm local em Keycloak 26.2.5 e obter token client credentials.
- [x] Validar token e claim `provider_id` reais, rejeitando assinatura adulterada.
- [x] Testar abrir carteira com client interno, processar aposta com token de provedor, consultar saldo, ledger, reconciliação e transação.
- [x] Testar token expirado emitido pelo Keycloak real e confirmar `401` em rota protegida; o teste restaura a validade original do realm.
- [x] Compartilhar o caso de uso e a idempotência com o consumidor SQS.
- [x] Expor métricas de resultados por status, replay, conflitos, retries SQS, candidatos ao redrive, atraso/publicação da outbox, latência e divergência da reconciliação em `/metrics`.

## 5. SQS, inbox e outbox

- [x] Implementar consumidor SQS que grava inbox e efeitos financeiros na mesma transação SQL.
- [x] Implementar publisher da outbox com reivindicação segura por múltiplas instâncias, backoff e recuperação de leases.
- [x] Testar reentrega, retry, DLQ e interrupção entre commit e remoção da mensagem.
- [x] Testar interrupção entre publicação e confirmação da outbox; republicações preservam o mesmo `eventId`.
- [x] Testar encerramento seguro e recuperação após reinício.

## 6. Concorrência e recuperação

- [x] Enviar a mesma aposta 50 vezes em paralelo, distribuídas por três processos PostgreSQL independentes, e confirmar um único débito, transação e par de eventos.
- [x] Disputar saldo de `100.00` com duas apostas de `80.00` em três processos independentes e confirmar que apenas uma é debitada.
- [x] Confirmar processamento de carteiras diferentes por processos independentes e validar cada saldo pelo ledger.
- [x] Repetir os cenários de idempotência e disputa de saldo com três processos independentes, cada um com seu próprio pool PostgreSQL.
- [x] Validar referência que chega depois da operação que depende dela, inclusive retomada após reinício.
- [x] Conferir saldo contra créditos menos débitos do ledger nos cenários concorrentes e de retomada.
- [x] Executar `go test -race ./...` e os testes PostgreSQL de integração com dependência real.

## 7. Readiness, observabilidade e demonstração

- [x] Fazer `/health/ready` consultar PostgreSQL, fila de entrada SQS e fila de eventos SQS.
- [x] Expor e testar `/metrics` com contadores e observações de latência.
- [x] Documentar como iniciar a stack, executar os fluxos principais e apresentar os testes de concorrência/recuperação em `docs/demo.md`.
- [x] Executar `go test -race ./...`, integrações PostgreSQL/LocalStack e token expirado contra Keycloak real.

## 8. Auditoria final do README

- [x] Validar códigos de moeda ISO 4217 reais e cobrir código desconhecido.
- [x] Testar início e encerramento do grafo Fx com PostgreSQL, Keycloak e LocalStack reais.
- [x] Testar a mesma operação entrando por HTTP e depois por SQS, sem segundo débito.
- [x] Completar logs correlacionados de HTTP, SQS e outbox, sem payload financeiro.
- [x] Atualizar `ARCHITECTURE.md` para refletir o código atual e documentar limitações.
- [x] Criar matriz de evidência em `docs/AUDITORIA_README.md`.
- [x] Encapsular `Money`, `Wallet`, `WagerTransaction` e `WalletLedgerEntry` com getters e reidratação validada.
- [x] Classificar falhas técnicas repetidas ao resolver referência; persistir retry, evento de retry e `FAILED` com `REFERENCE_RESOLUTION_FAILED` no limite.
- [x] Finalizar política de falha terminal da outbox após dez tentativas.
- [x] Corrigir o transporte JSON dos testes concorrentes entre processos para reconstruir `Money` imutável com valor e moeda explícitos.
- [x] Executar testes de integração PostgreSQL/SQS/LocalStack com `-race` nesta validação.
- [x] Corrigir a auditoria para registrar que policies do broker não são provisionadas nem testadas pelo Compose local.

## Conclusão

Os fluxos da aplicação foram implementados e verificados com PostgreSQL, Keycloak e LocalStack locais. A autorização por policy do broker permanece parcial: o Compose não cria policies SQS nem testa acessos permitidos/negados; esse item precisa de configuração no ambiente de deploy. `docs/demo.md` contém os comandos e fluxos de demonstração.
