# Como estou pensando o modelo de dados

Este documento explica a ideia inicial das tabelas com minhas palavras. Estou começando pelas informações financeiras principais. As migrations ainda serão criadas depois que eu revisar os campos e as regras.

## Relação entre jogador e carteira

O Keycloak é responsável por identificar e autenticar o jogador. No banco da aplicação, vou guardar o `player_id` que identifica esse usuário. Não preciso criar uma tabela de jogadores só para repetir informações que já ficam no Keycloak.

Um jogador pode ter mais de uma carteira, desde que cada carteira seja de uma moeda diferente. Por exemplo, o mesmo jogador pode ter uma carteira em BRL e outra em USD, mas não pode ter duas carteiras em BRL.

Cada carteira terá seu próprio ID. Esse ID identifica a carteira dentro da aplicação; ele é diferente do `player_id`.

## Tabela `wallets`

A tabela `wallets` guarda as carteiras dos jogadores. Cada linha representa a carteira de um jogador em uma moeda e guarda o saldo que ele tem naquela carteira.

De início, imagino que ela precise guardar:

- o ID da carteira;
- o `player_id` do Keycloak;
- o tipo da moeda;
- o saldo disponível.

Também vou avaliar campos como versão e datas de criação e atualização, porque ajudam a controlar as mudanças no saldo e a acompanhar quando a carteira foi alterada.

A regra principal é: o mesmo jogador só pode ter uma carteira para cada moeda. O banco deve garantir isso, além de impedir que o saldo fique negativo.

## Tabela `wager_transactions`

Essa tabela registra as operações recebidas ou feitas pelo sistema, como `BET`, `WIN`, `LOSS`, `REFUND`, `ROLLBACK` e a abertura interna da carteira (`OPENING`). Cada operação se relaciona com a carteira pelo ID próprio da carteira.

Por exemplo, se uma aposta acontecer na carteira de ID `447`, a operação guarda esse ID. Assim eu consigo saber a qual carteira aquela aposta pertence.

Também preciso registrar informações que permitam reconhecer uma repetição da mesma operação, como o provedor, o ID externo da operação, a chave de idempotência, o tipo da operação e seu estado.

O histórico da operação é importante mesmo quando ela não muda o saldo. Pelo README, uma operação `LOSS` deve ficar registrada, embora não altere o saldo e não gere um lançamento no ledger. Operações rejeitadas também precisam ter resultado auditável. Operações pendentes que dependem de uma referência precisam ser salvas para que outra instância possa retomá-las depois de uma interrupção.

## Tabela `wallet_ledger_entries`

O ledger é o histórico das mudanças que realmente aconteceram no saldo. Quando uma operação altera uma carteira, o ledger registra a movimentação, com a carteira relacionada e a operação que causou a mudança.

Por exemplo, se a carteira tinha `120.00 BRL` e uma aposta aceita retirou `20.00 BRL`, o lançamento registra o saldo anterior (`120.00`), o valor retirado (`20.00`) e o saldo depois da operação (`100.00`).

Uma operação que não altera o saldo, como `LOSS`, não cria lançamento no ledger. Lançamentos existentes não devem ser editados ou apagados; correções financeiras devem acontecer por novas operações.

## Como pretendo salvar uma aposta

Quando uma aposta é aceita, o sistema valida o saldo da carteira. Se houver saldo suficiente, atualiza a carteira e registra a operação e o lançamento no ledger. Esses dados precisam ser confirmados juntos no banco para que não exista saldo atualizado sem histórico, nem histórico sem saldo atualizado.

Se for necessário avisar outros componentes, o sistema também guarda o evento na outbox durante essa mesma confirmação. Um worker publica esse aviso depois. A outbox não executa a aposta nem atualiza o saldo; ela ajuda a não perder o aviso se a aplicação parar depois do commit.

Se não houver saldo suficiente, a operação fica registrada como rejeitada, mas não há débito nem lançamento financeiro no ledger.

## Relações resumidas

```text
Keycloak fornece o player_id
        │
        └── wallets (uma carteira por jogador e moeda)
              ├── wager_transactions (operações relacionadas à carteira)
              └── wallet_ledger_entries (movimentações confirmadas)
```

`wager_transactions` e `wallet_ledger_entries` guardam o ID da carteira. O ledger também guarda o ID da operação que causou a movimentação. Dessa forma, consigo consultar tanto o histórico de uma carteira quanto a operação que originou cada mudança de saldo.

## Próximas decisões

Antes de criar as migrations, ainda vou definir os campos exatos e as regras de banco para cada tabela. Também preciso detalhar as referências de `REFUND` e `ROLLBACK`, a identificação do provedor e quais dados da resposta original serão guardados para responder a uma repetição da operação.
