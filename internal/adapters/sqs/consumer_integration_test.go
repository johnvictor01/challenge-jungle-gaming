package sqs

// TODO: com LocalStack ou MiniStack real, testar processamento e deduplicação
// de mensagens repetidas usando a inbox persistente.
// TODO: testar retry e encaminhamento para DLQ conforme a política definida.
// TODO: simular interrupção depois do commit no banco e antes da remoção da
// mensagem; verificar que a reentrega não duplica a movimentação.
// TODO: testar publisher concorrente da outbox e recuperação após falha de
// publicação ou confirmação.
// TODO: testar encerramento seguro do consumidor e recuperação após reinício.
