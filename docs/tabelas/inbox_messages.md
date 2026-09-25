# Tabela `inbox_messages`

## Para que ela serve

Essa tabela funciona como um recibo das mensagens que chegaram pelo SQS. O SQS pode entregar a mesma mensagem mais de uma vez. Com a inbox, o sistema consegue verificar se já tratou aquele ID de mensagem e evitar repetir o trabalho daquela entrega.

A inbox não substitui a idempotência da operação. O ID da mensagem identifica uma entrega do SQS; a chave de idempotência identifica a operação financeira. A mesma operação pode chegar em mensagens diferentes ou também por HTTP.

## Como ela se relaciona

Cada registro pertence a um consumidor e identifica uma mensagem do SQS. Pode também guardar o ID da operação relacionada. Quando a mensagem for tratada, o registro da inbox deve ser confirmado junto com as mudanças financeiras e com os eventos correspondentes.

## Informações que deve guardar

- `consumer_name`: nome do consumidor que está tratando a mensagem.
- `message_id`: identidade fornecida no envelope da mensagem.
- `payload_hash`: permite perceber se o mesmo ID reapareceu com conteúdo diferente.
- `received_at`: quando a mensagem foi recebida.
- `completed_at`: quando o tratamento ficou durável no banco.
- referência opcional à operação relacionada.

## Regras principais

- A combinação de consumidor e ID da mensagem deve ser única.
- O sistema só remove a mensagem da fila depois que o tratamento durável foi confirmado no banco.
- Uma rejeição de negócio confirmada é terminal e permite remover a mensagem.
- Falhas transitórias devem permitir nova tentativa sem duplicar efeitos financeiros.
- Se o ID já existir com outro conteúdo, o sistema deve detectar a divergência em vez de tratar como uma repetição normal.

