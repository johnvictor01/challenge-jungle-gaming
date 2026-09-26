# SQS, inbox e outbox — base para a próxima etapa

Esta página registra a forma que a integração deve seguir conforme o README. O adapter SQS ainda não está implementado; os itens abaixo permanecem pendentes até existir consumidor e publisher executáveis.

## Entrada pela inbox

O consumidor deve validar envelope e payload, usar `data.idempotencyKey` e gerar o mesmo hash canônico do endpoint HTTP. A identidade durável da entrega é `(consumer_name, message_id)`. Inbox, operação, saldo, ledger e eventos correspondentes precisam ser confirmados no mesmo `UnitOfWork`. Só remover a mensagem SQS depois do commit. Rejeições de negócio persistidas são terminais; falhas transitórias devem permitir retry.

## Publicação pela outbox

O publisher deverá reivindicar eventos em lotes com lock concorrente (`FOR UPDATE SKIP LOCKED`) e lease com expiração. Publicação acontece depois do commit que criou o evento. Depois de publicar, marcar o registro como publicado; se houver interrupção entre envio e marcação, republicar com o mesmo `eventId`. Falhas devem incrementar tentativas e agendar backoff; mensagens permanentes seguem para DLQ conforme política documentada.

## Encerramento e recuperação

Em `SIGTERM`, parar novas leituras e concluir ou liberar o trabalho em andamento. Testar queda depois do commit antes do delete da mensagem, queda depois da publicação antes da confirmação, retomada por outra instância e duas instâncias concorrentes.

## Pendências de implementação

- [ ] Adapter SQS com LocalStack ou MiniStack.
- [ ] Repositório inbox e integração no `UnitOfWork`.
- [ ] Reivindicação, lease, backoff e confirmação da outbox.
- [ ] Política e configuração de DLQ/visibility timeout.
- [ ] Provisionamento automático de filas.
- [ ] Testes reais de reentrega, interrupção, recuperação e concorrência.
