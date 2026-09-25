# Roteiro de testes

Este arquivo acompanha os testes pedidos no README. Os testes unitários de `Money`, `Wallet`, `WagerTransaction` e `WalletLedgerEntry` estão escritos e passaram localmente usando `GO111MODULE=off go test` dentro de `internal/domain`. O repositório ainda não tem `go.mod`, então o comando habitual `go test ./...` não funciona por enquanto. Os demais itens continuam como trabalho futuro.

## 1. Domínio

- [x] `ParseMoney`: valor válido convertido para unidades mínimas.
- [x] `ParseMoney`: entradas inválidas rejeitadas.
- [x] `ParseMoney`: overflow rejeitado.
- [x] Valor zero associado a uma moeda.
- [x] Soma na mesma moeda e overflow.
- [x] Subtração exata, resultado negativo interno e overflow.
- [x] Negação e overflow no menor valor representável.
- [x] Comparação entre valores da mesma moeda.
- [x] Soma, subtração e comparação rejeitam moedas diferentes com erro classificável.
- [x] Serialização mantém duas casas decimais e a moeda.
- [x] Invariantes de carteira: criação, reidratação, saldo não negativo, moeda, jogador, versão, crédito, débito, overflow e movimentações inválidas.
- [x] Transições de estado de `WagerTransaction`, recusa, falha e proteção dos estados terminais.
- [x] Regras de criação, valor e referência para `BET`, `WIN`, `LOSS`, `REFUND` e `ROLLBACK`.
- [ ] Mesmo idempotency key com payload diferente deve gerar conflito.
- [x] Política de valores zero conforme o tipo de operação (`LOSS` aceita zero; os demais tipos externos exigem valor positivo).
- [x] Abertura interna (`OPENING`): identidade, metadados internos, saldo e estado processado.
- [ ] Eventos da abertura interna são gravados na outbox junto com carteira e ledger.
- [x] `WalletLedgerEntry`: criação de crédito/débito, direção, valor, moeda, cálculo, saldo, overflow, identificadores e reidratação; ver `internal/domain/wallet_ledger_entry_test.go`.

## 2. PostgreSQL e migrations

- [ ] Migrations sobem e descem no PostgreSQL real.
- [ ] Constraints protegem unicidade de carteira por jogador e moeda e demais invariantes do schema.
- [ ] PostgreSQL garante unicidade de (carteira, transação) e impede UPDATE/DELETE no ledger.
- [ ] Alteração financeira e registro correspondente são atômicos.
- [ ] Inbox persiste a chave idempotente e permite reconhecer reentrega.
- [ ] Outbox persiste eventos e permite publicação concorrente sem duplicação indevida.
- [ ] Falha e reinício preservam transações, pendências e consistência.

## 3. HTTP, autenticação e autorização

Ainda não há adaptador HTTP implementado. Quando ele existir, cobrir:

- [ ] Integração com o IdP real: credencial ausente, inválida e expirada são rejeitadas.
- [ ] Um provedor não consulta nem reproduz operações de outro provedor.
- [ ] Operações internas só podem ser chamadas por quem tem autorização.
- [ ] Acesso não autorizado não altera saldo e não revela dados financeiros.
- [ ] A mesma operação recebida por HTTP e SQS não movimenta o saldo duas vezes.

## 4. SQS, inbox e outbox

Ainda não há consumidor ou publisher implementado. Quando existirem, cobrir:

- [ ] Mensagem repetida é deduplicada usando a persistência da inbox.
- [ ] Falha durante processamento permite retry e recuperação.
- [ ] Mensagem que excede tentativas segue a política de DLQ.
- [ ] Consumidor interrompido após commit e antes de apagar a mensagem suporta reentrega.
- [ ] Dois publishers concorrentes não publicam incorretamente o mesmo evento da outbox.
- [ ] Evento publicado pode ser recuperado após falha entre publicação e confirmação.

## 5. Concorrência e recuperação

- [ ] Enviar a mesma aposta 50 vezes em paralelo resulta em um único débito.
- [ ] Duas apostas de `80.00` disputando saldo `100.00`: somente uma é debitada.
- [ ] Operações em carteiras diferentes podem ocorrer em paralelo.
- [ ] Repetir cenários relevantes com pelo menos três instâncias independentes.
- [ ] `REFUND` ou `ROLLBACK` antes da referência é resolvido depois ou rejeitado conforme expiração.
- [ ] Reiniciar a aplicação preserva idempotência, pendências e consistência financeira.
- [ ] Saldo da carteira coincide com créditos menos débitos no ledger.
- [ ] Deduplicação é comprovada por recebimentos repetidos reais.
- [ ] Executar `go test -race` nos pacotes aplicáveis.

## 6. Inicialização e encerramento

- [ ] Composição Fx inicia os componentes necessários.
- [ ] Encerramento Fx para os workers e libera conexões e demais recursos.

## Próximos passos sugeridos

1. Criar o caso de uso que coordena a transação, a carteira e o ledger, com testes para sucesso, recusa, repetição e conflito de idempotência.
2. Conectar esse caso de uso aos repositórios PostgreSQL e garantir que saldo, transação e ledger sejam confirmados juntos.
3. Depois cobrir outbox/inbox e integração com SQS, e então os fluxos HTTP/autenticação e concorrência descritos acima.

Os testes unitários de domínio marcados como feitos foram executados e passaram. Os itens restantes ainda não foram implementados ou verificados. Os arquivos de teste de integração para PostgreSQL e SQS são apenas lembretes com comentários, não testes executáveis.
