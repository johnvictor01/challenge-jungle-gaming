# Documentação da implementação

Este diretório reúne as explicações de cada parte do projeto. A ordem acompanha o fluxo de dados: contrato e autenticação, casos de uso, persistência e mensageria.

| Documento | O que explica | Estado |
| --- | --- | --- |
| [HTTP e Keycloak](../api-http-oidc.md) | Rotas, papéis, claims OIDC e execução local | Implementado; integração end-to-end passa localmente |
| [Aplicação e domínio](aplicacao.md) | Casos de uso, operações e idempotência | Implementado com testes unitários |
| [PostgreSQL](postgresql.md) | Unit of Work, locks, repositórios e migrations | Implementado e verificado em PostgreSQL |
| [SQS, inbox e outbox](sqs-outbox.md) | Contrato planejado para entrada e publicação | Base preparada; adapter pendente |
| [Arquitetura geral](../../ARCHITECTURE.md) | Decisões, restrições e fases | Atualizado por etapa |
| [Roteiro de testes](../TODO_TESTES.md) | Cobertura realizada e pendências conforme o README | Atualizado |

Ao concluir uma etapa, atualizar o documento correspondente, o roteiro de testes e esta tabela. Não marcar integração como concluída até existir teste com o serviço real indicado no README.
