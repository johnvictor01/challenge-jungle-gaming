package postgres

// TODO: com PostgreSQL real, testar aplicação e reversão das migrations.
// TODO: testar constraints de unicidade e integridade definidas no schema.
// TODO: testar que o ledger não permite alterar ou apagar lançamentos já
// registrados.
// TODO: testar atomicidade entre alteração do saldo, transação e lançamento
// do ledger.
// TODO: testar persistência da inbox e deduplicação após reentrega.
// TODO: testar persistência e reivindicação concorrente de eventos da outbox.
// TODO: testar que falhas e reinicializações não deixam saldo e ledger
// divergentes.
