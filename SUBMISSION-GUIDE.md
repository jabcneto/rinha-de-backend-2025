# Estrutura para PR na Rinha de Backend 2025

Crie esta estrutura no fork do repositório da Rinha:

```
participantes/
└── jabcn-go-clean/
    ├── docker-compose.yml
    ├── info.json  
    ├── README.md
    ├── nginx.conf
    ├── postgresql.conf
    └── sql/
        └── init.sql
```

## Arquivos a incluir:

1. **docker-compose.yml** - Versão com imagens públicas
2. **info.json** - Metadados da submissão
3. **README.md** - README-submission.md renomeado
4. **nginx.conf** - Configuração do Nginx
5. **postgresql.conf** - Configurações do PostgreSQL
6. **sql/init.sql** - Scripts de inicialização do banco

## ⚠️ IMPORTANTE:
- NÃO incluir código fonte no PR
- Código fonte deve estar em repositório público separado
- Usar apenas as imagens do Docker Hub
