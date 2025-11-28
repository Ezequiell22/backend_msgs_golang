# Backend de Links Efêmeros (Go + Redis)

Serviço REST para compartilhar mensagens com modelo one‑time link: o cliente cifra localmente e o servidor armazena apenas o ciphertext. A leitura retorna o ciphertext e apaga a mensagem (burn‑after‑read).

## Visão Geral
- Modelo: código efêmero + mensagem cifrada pelo cliente.
- Servidor não vê plaintext nem chave; apenas recebe e entrega ciphertext.
- Armazenamento: Redis com TTL para placeholders e mensagens.
- Sem autenticação; cabeçalhos de privacidade e logs sem conteúdo sensível.

## Endpoints
- `POST /code` → gera e reserva um `code` único com TTL de placeholder.
- `PUT /message/:code` → anexa o `ciphertext` (base64 de `IV(12B)+ciphertext`) e atualiza TTL de mensagem.
- `GET /message/:code` → retorna o `ciphertext` (text/plain) e apaga imediatamente (burn‑after‑read).
- `GET /health` → 200 OK.

Referências:
- Reserva de código: `internal/server/server.go:64-88`
- PUT de mensagem: `internal/server/server.go:106-147`
- GET e burn‑after‑read: `internal/server/server.go:149-166`
- Redis storage: `internal/storage/redis/redis.go:23-64`

## Execução com Docker
Requisitos: Docker Desktop ativo.

```bash
# Build e subir serviços
docker compose build
docker compose up -d

# Logs do backend
docker compose logs -f backend

# Parar
docker compose down
```

O backend expõe `http://localhost:8080` e o Redis `localhost:6379`.

## Variáveis de Ambiente
Backend:
- `ADDR` (default `:8080`)
- `PLACEHOLDER_TTL` (default `30m`)
- `MESSAGE_TTL` (default `24h`)
- `MAX_BODY_BYTES` (default `1048576`)
- `READ_TIMEOUT`, `READ_HEADER_TIMEOUT`, `WRITE_TIMEOUT`, `IDLE_TIMEOUT`
- `LOG_LEVEL` (`debug|info|warn|error`, default `info`)

Redis:
- `REDIS_ADDR` (Compose usa `redis:6379`)
- `REDIS_PASSWORD` (opcional)
- `REDIS_DB` (default `0`)
- `REDIS_TLS` (`1` para habilitar)

Estas variáveis já estão definidas no `docker-compose.yml` e podem ser ajustadas conforme necessidade.

## Formato do Ciphertext (PUT)
- Header: `Content-Type: text/plain`
- Body: string base64 do buffer `IV(12 bytes) + ciphertext` gerado por AES‑GCM no cliente.
- O servidor valida formato básico (base64) e não decodifica nem decifra.

## Testes Rápidos (curl)
```bash
# Gerar code
curl -s -X POST http://localhost:8080/code
# => 201 Created + {"code":"X7a9qL"}

# Anexar ciphertext (exemplo)
curl -i -X PUT http://localhost:8080/message/X7a9qL \
  -H 'Content-Type: text/plain' \
  --data 'BASE64_IV_PLUS_CIPHERTEXT'
# => 204 No Content

# Ler e queimar (primeira leitura retorna o base64)
curl -i http://localhost:8080/message/X7a9qL
# => 200 OK + body com base64

# Segunda leitura
curl -i http://localhost:8080/message/X7a9qL
# => 404 Not Found
```

## Segurança e Privacidade
- Cliente cifra localmente; servidor não possui chave.
- Recomendado compartilhar links com o secret no fragmento `#` (não enviado ao servidor).
- Headers de privacidade: `Referrer-Policy: no-referrer`, `Cache-Control: no-store`, `X-Content-Type-Options: nosniff`, `Pragma: no-cache`.
- Logging estruturado sem conteúdo sensível (somente eventos e níveis).

## Arquitetura
- `cmd/server/main.go` → entrypoint; lê env e inicia servidor.
- `internal/server/server.go` → HTTP server, rotas e timeouts.
- `internal/storage/redis/redis.go` → integração Redis (SETNX, Lua atômico, GETDEL).
- `internal/log/log.go` → logger JSON com níveis.
- `Dockerfile` → build multi‑stage, runtime distroless.
- `docker-compose.yml` → serviços `backend` e `redis`, envs e portas.
- `go.mod` → dependências (`github.com/redis/go-redis/v9`).

## Execução Local (sem Docker)
Com Go 1.21+:
```bash
# Subir um Redis local em 6379 ou ajustar REDIS_ADDR
export REDIS_ADDR=localhost:6379
export PLACEHOLDER_TTL=30m
export MESSAGE_TTL=24h
export ADDR=:8080

go run ./cmd/server
```

## Troca de Armazenamento
A interface `Storage` (`internal/storage/storage.go`) permite trocar Redis por outro backend mantendo o contrato:
- `ReserveCode(ctx, code, ttl)`
- `AttachCipher(ctx, code, ciphertext, ttl)`
- `GetAndDelete(ctx, code)`

## Boas Práticas Adicionais
- Rate limiting no ingress/reverse proxy.
- Limite de tamanho do ciphertext via `MAX_BODY_BYTES`.
- TLS no acesso externo ao backend e ao Redis.
