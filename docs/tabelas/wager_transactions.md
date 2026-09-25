# Tabela `wager_transactions`

## Para que ela serve

Essa tabela registra as operações que o sistema recebeu ou criou, como `BET`, `WIN`, `LOSS`, `REFUND`, `ROLLBACK` e a abertura interna da carteira (`OPENING`). Ela conta o que aconteceu com cada pedido, mesmo quando ele não altera o saldo.

Por exemplo, se chega uma aposta para a carteira de ID `447`, a operação aponta para essa carteira. Se a aposta for recusada por falta de saldo, ainda preciso guardar a decisão e o motivo.

## Como ela se relaciona

Cada operação se relaciona com a carteira pelo `wallet_id`. Operações externas também guardam o provedor e o ID da operação que veio dele. Operações de reversão guardam a referência à operação que estão tentando desfazer.

Uma operação que realmente altera o saldo também se relaciona com o lançamento criado no ledger. Uma operação pode existir sem lançamento, como `LOSS` ou uma operação rejeitada.

## Informações que deve guardar

- `id`: identificador interno da operação.
- `origin`: informa se a operação veio de fora ou foi criada internamente.
- `wallet_id` e `player_id`: carteira e jogador relacionados.
- `provider_id` e `external_transaction_id`: identificação da operação externa e de quem a enviou, quando aplicável.
- `idempotency_key` e `payload_hash`: permitem reconhecer uma repetição e detectar quando a mesma chave chega com conteúdo diferente.
- `round_id`, `game_id`, `kind` e `money`: contexto e valor da operação.
- `reference_external_transaction_id` e referência interna resolvida: dados usados em `REFUND`, `ROLLBACK` e operações que dependem de outra operação.
- `status`: estado atual, como pendente, aguardando referência, processada, rejeitada ou falha permanente.
- `failure_code`: motivo estável quando a operação for rejeitada ou falhar.
- resultado original devolvido ao provedor, incluindo o saldo observado no processamento, para que um replay receba a resposta original.
- datas de criação, atualização e processamento; campos de tentativa e próximo processamento para pendências retomáveis.

Os valores de dinheiro serão guardados em unidades mínimas (`amount_minor`), junto com a moeda (`currency`). O ID da carteira, o ID do jogador e a moeda precisam corresponder à carteira referenciada.

## Regras principais

- A mesma operação externa não pode ser aplicada duas vezes, mesmo que chegue por HTTP e SQS ou após reiniciar a aplicação.
- Reutilizar uma chave de idempotência com outro conteúdo deve resultar em conflito.
- Operações pendentes que precisam ser retomadas devem estar persistidas; não posso depender somente da memória de uma instância.
- `OPENING` é uma operação interna e não pode ser enviada pelo provedor.
- `LOSS` é registrada como processada, mas não altera saldo nem gera lançamento no ledger.
- Operações rejeitadas ficam registradas com seu código de rejeição, mas não geram movimentação no ledger.
- Uma operação terminal não deve ser processada de novo.
- Para uma mesma operação de origem, o banco permite no máximo uma reversão bem-sucedida entre `REFUND` e `ROLLBACK`. Um `ROLLBACK` pode, por sua vez, referenciar um `REFUND` processado, conforme as regras do README.
- Quando a referência interna estiver resolvida, o banco confere que ela pertence ao mesmo provedor, jogador, carteira, moeda e rodada; também confere o tipo permitido e que uma reversão tenha o mesmo valor da operação referenciada.
