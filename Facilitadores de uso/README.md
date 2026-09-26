# Facilitadores de uso

Esta pasta reúne duas ferramentas locais para iniciar e testar a API:

- `dev_menu.py`: menu interativo no terminal para subir dependências, abrir carteira, enviar operações e consultar saldo/ledger.
- `challenge-jungle-gaming.insomnia.json`: coleção de requisições para importar no Insomnia.

## Menu de terminal

Na raiz do repositório, execute:

```sh
python3 "Facilitadores de uso/dev_menu.py"
```

Escolha `1` para iniciar PostgreSQL, aplicar migrations, iniciar Keycloak e LocalStack e subir a API. O menu apresenta as outras opções para criar carteira, enviar e repetir operação, consultar carteira/ledger, reconciliar, verificar health/métricas e parar os serviços.

O script lê `.env` se existir; caso contrário, usa `.env.example`. Os serviços locais mantêm os dados nos volumes Docker ao parar.

## Insomnia

Se o aplicativo ainda não estiver instalado, baixe a versão Linux pela [página oficial do Insomnia](https://insomnia.rest/download) (em Ubuntu, escolha **Download for Others** e o pacote `.deb`; NSIS é para Windows). A coleção abaixo é o arquivo de requisições do Insomnia; o instalador do aplicativo não é armazenado neste repositório.

1. Inicie os serviços pelo menu acima.
2. No Insomnia, clique em **Import** e selecione `challenge-jungle-gaming.insomnia.json`.
3. Na pasta **01 - Keycloak - obter tokens**, envie os pedidos dos clients `internal-service` e `provider-a`.
4. Copie `access_token` da resposta interna para `internal_token` e o token do provedor para `provider_token` no ambiente **Base Environment**.
5. Envie **Criar carteira** e copie o `id` da resposta para `wallet_id`.
6. Envie **Enviar aposta BET**. Copie `transactionId` para `transaction_id` e teste o replay repetindo **Repetir aposta (idempotência)** sem trocar a chave nem o ID externo.
7. Consulte carteira, ledger, reconciliação, transação e health/métricas.

A coleção cobre todos os endpoints HTTP do README. Os endpoints SQS são filas, não URLs HTTP; eles são exercitados pelos testes de integração do projeto. Cada nova aposta deve usar novo `external_transaction_id` e `idempotency_key`. Para o replay, mantenha os dois iguais.

Os secrets na coleção são valores locais de demonstração, iguais aos do realm de teste do repositório. Não os use em ambientes externos.
