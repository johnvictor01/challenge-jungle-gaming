# Tabela `outbox_events`

## Para que ela serve

Essa tabela guarda avisos que o sistema precisa publicar depois de confirmar uma mudança no banco. Por exemplo: “a aposta foi processada” ou “o saldo da carteira mudou”.

A outbox não recebe nem processa a aposta e não atualiza o saldo. Ela evita que o sistema confirme uma mudança financeira e depois perca o aviso porque caiu antes de publicá-lo.

## Como ela se relaciona

Cada evento tem uma identidade própria e aponta para o agregado relacionado, normalmente uma carteira ou uma operação. O evento é criado na mesma transação SQL que confirma a mudança financeira. Um worker separado busca os eventos ainda não publicados e envia-os ao destino configurado.

## Informações que deve guardar

- `event_id`: ID estável do evento, preservado nas tentativas e republicações.
- `aggregate_id`: ID da carteira ou operação relacionada.
- `event_type` e versão: tipo e formato do evento.
- `correlation_id` e `causation_id`, quando aplicáveis: ajudam a acompanhar a origem do evento.
- `payload`: cópia dos dados do evento no momento em que ele foi criado.
- `occurred_at`: quando o evento aconteceu.
- número de tentativas e `next_attempt_at`: controlam as novas tentativas.
- dados de claim/lease: permitem que vários publishers dividam o trabalho e recuperem eventos abandonados.
- `published_at`: informa quando houve confirmação da publicação.

## Regras principais

- O evento só pode ser publicado depois do commit que originou o evento.
- O payload guardado deve ser um snapshot e não mudar entre tentativas.
- Identidade, tipo, versão e payload do evento são imutáveis depois da criação; tentativas, lease e publicação podem ser atualizados pelo worker.
- Se houver uma falha depois de publicar e antes de marcar o evento como publicado, pode haver republicação; por isso o consumidor deve reconhecer o `event_id` e tratar duplicatas com segurança.
- Vários publishers podem trabalhar ao mesmo tempo, sem publicar o mesmo registro como trabalho independente.
- Falhas de publicação devem gerar novas tentativas com backoff.
