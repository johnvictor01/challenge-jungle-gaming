# Tabela `wallet_ledger_entries`

## Para que ela serve

Essa tabela é o livro-razão da carteira: guarda o histórico de cada mudança que realmente aconteceu no saldo. Ela me permite entender de onde veio o saldo atual e conferir se ele bate com as movimentações.

## Como ela se relaciona

Cada lançamento aponta para a carteira (`wallet_id`) e para a operação (`transaction_id`) que causou a mudança. Assim consigo consultar as movimentações de uma carteira e também descobrir qual operação originou cada lançamento.

## Informações que deve guardar

- `id`: identificador do lançamento.
- `wallet_id`: carteira movimentada.
- `transaction_id`: operação que causou a movimentação.
- `direction`: indica se o valor foi um débito ou crédito.
- valor e moeda movimentados.
- `balance_before`: saldo antes da movimentação.
- `balance_after`: saldo depois da movimentação.
- `created_at`: quando o lançamento foi registrado.

No banco, valor e saldos serão guardados em unidades mínimas, junto com a moeda.

## Exemplo

Se a carteira tem `120.00 BRL` e uma aposta aceita retira `20.00 BRL`, o lançamento guarda o débito de `20.00 BRL`, o saldo anterior de `120.00 BRL` e o saldo posterior de `100.00 BRL`.

## Regras principais

- Só existe lançamento quando há mudança real no saldo. `LOSS` e operações rejeitadas não geram lançamento.
- A combinação de carteira e operação deve ser única, para não lançar duas vezes a mesma movimentação.
- O saldo posterior deve corresponder ao saldo anterior mais ou menos o valor, conforme a direção.
- O ledger é append-only: lançamentos não são editados nem apagados. Para corrigir um valor, o sistema cria uma nova operação e um novo lançamento.
- A atualização da carteira e a gravação do lançamento devem acontecer no mesmo commit do banco.

## Como vou testar

Nos testes de domínio, vou conferir se crédito soma o valor e débito subtrai, sempre mantendo a moeda. Também vou testar direção inválida, valor zero ou negativo, saldo negativo, moeda diferente, conta incorreta entre os saldos, débito maior que o saldo e overflow.

Vou conferir também se não consigo criar um lançamento sem ID da carteira ou da operação e se a reidratação mantém a data registrada. No PostgreSQL, vou testar que a mesma operação não cria dois lançamentos para a carteira e que um lançamento existente não pode ser editado ou apagado.
