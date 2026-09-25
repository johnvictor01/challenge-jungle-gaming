# Tabela `wallets`

## Para que ela serve

A tabela `wallets` guarda as carteiras dos jogadores. Cada linha representa uma carteira de um jogador em uma moeda e informa quanto ele tem disponível naquela carteira.

O jogador pode ter várias carteiras, mas só pode ter uma carteira para cada moeda. Por exemplo, ele pode ter uma carteira em BRL e outra em USD.

## Como ela se relaciona

O `player_id` identifica o jogador autenticado pelo Keycloak. Cada carteira também tem seu próprio `id`, que é o identificador usado pelas operações e pelo ledger. O ID da carteira é diferente do ID do jogador.

## Informações que deve guardar

- `id`: identificador próprio da carteira.
- `player_id`: identificador do jogador vindo do Keycloak.
- `currency`: moeda da carteira, como BRL.
- `balance_minor`: saldo em unidades mínimas da moeda, como centavos. Assim não preciso usar número decimal aproximado para calcular dinheiro.
- `version`: número que aumenta quando o saldo muda.
- `created_at` e `updated_at`: datas de criação e da última alteração.

## Regras principais

- A combinação de jogador e moeda deve ser única.
- O saldo não pode ficar negativo.
- A moeda da operação deve ser igual à moeda da carteira.
- Toda carteira deve ser criada junto com uma operação interna `OPENING`, na mesma transação SQL. O saldo inicial pode ser zero; se for maior que zero, também deve existir um lançamento de crédito no ledger.
- O saldo, a operação e o lançamento correspondente precisam ser confirmados juntos no banco.
