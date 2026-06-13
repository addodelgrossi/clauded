# clauded

> **Uma API HTTP sobre o Claude Code headless, usando a sua assinatura Pro/Max.**

[![CI & Release](https://github.com/addodelgrossi/clauded/actions/workflows/release.yml/badge.svg)](https://github.com/addodelgrossi/clauded/actions/workflows/release.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/addodelgrossi/clauded.svg)](https://pkg.go.dev/github.com/addodelgrossi/clauded)
[![Go Report Card](https://goreportcard.com/badge/github.com/addodelgrossi/clauded)](https://goreportcard.com/report/github.com/addodelgrossi/clauded)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

`clauded` é um daemon HTTP em Go (convenção Unix: *Claude + d*, como `sshd`/`dockerd`)
que embrulha o **Claude Code em modo headless** (`claude -p`) e o expõe como uma
**API REST/streaming**. Ele roda na sua máquina, executa prompts agênticos em
projetos locais e devolve o resultado por JSON ou Server-Sent Events.

O ponto central: o `clauded` autentica o Claude Code com o **token OAuth da sua
assinatura** (`CLAUDE_CODE_OAUTH_TOKEN`), e **não** com a `ANTHROPIC_API_KEY`
paga por token — você usa o limite que já paga. Para acesso remoto, ele fica
atrás de um **Cloudflare Tunnel** (conexão saída-apenas, sem abrir portas no
firewall), protegido por um Bearer token.

---

## ⚠️ Aviso de segurança

**Este serviço executa comandos e edita arquivos na sua máquina.** Trate-o como
um acesso privilegiado:

- **Proteja os tokens.** `CLAUDED_API_TOKEN` e `CLAUDE_CODE_OAUTH_TOKEN` dão
  acesso ao serviço e à sua assinatura. Nunca os commite, nunca os exponha em
  logs. O `clauded` jamais loga esses valores.
- **Use a allowlist de diretórios.** Configure `CLAUDED_ALLOWED_ROOTS` para
  restringir quais pastas um cliente pode ler/editar. Caminhos fora delas (após
  resolver symlinks e `..`) são rejeitados com `403`.
- **Modos de permissão perigosos ficam bloqueados por padrão.**
  `bypassPermissions` e `dontAsk` só funcionam com `CLAUDED_ALLOW_DANGEROUS=true`.
- **Bind em `127.0.0.1`.** O acesso externo deve vir só pelo túnel Cloudflare.
  Nunca exponha a porta diretamente na internet.

---

## Como funciona

```
┌─────────────┐        HTTPS         ┌───────────────────────────────────────┐
│  Cliente    │ ───────────────────▶ │  Cloudflare Edge                       │
│  externo    │  Bearer <API_TOKEN>  │  clauded.seudominio.com                │
└─────────────┘                      └──────────────────┬────────────────────┘
                                                        │ túnel saída-apenas
                                          ┌─────────────▼──────────────┐
                                          │  cloudflared (na máquina)  │
                                          └─────────────┬──────────────┘
                                                        │ http://127.0.0.1:8787
                                          ┌─────────────▼──────────────┐
                                          │      clauded (Go)          │
                                          │  auth · rate limit ·       │
                                          │  allowlist · monta argv    │
                                          └─────────────┬──────────────┘
                                                        │ exec.CommandContext
                                          ┌─────────────▼──────────────┐
                                          │  claude -p ... (subprocess)│
                                          │  env: CLAUDE_CODE_OAUTH_…  │
                                          │  cwd: workdir do projeto   │
                                          └────────────────────────────┘
```

Uma requisição `POST /v1/runs` é autenticada, validada, ganha um slot no
semáforo de concorrência, é traduzida para o `argv` do `claude` (sem shell, sem
injeção), executada com o `cwd` e o env corretos, e a saída é devolvida como
JSON único ou stream SSE.

---

## Instalação

### Binário pré-compilado

Baixe o arquivo da [última release](https://github.com/addodelgrossi/clauded/releases)
para a sua plataforma (`darwin/arm64`, `darwin/amd64`, `linux/amd64`,
`linux/arm64`, `windows/amd64`), extraia e mova `clauded` para o `PATH`:

```bash
tar -xzf clauded_*_darwin_arm64.tar.gz
install clauded ~/.local/bin/
```

### Via `go install`

```bash
go install github.com/addodelgrossi/clauded/cmd/clauded@latest
```

### Compilando do código

```bash
git clone https://github.com/addodelgrossi/clauded
cd clauded
make build      # gera dist/clauded
```

---

## Pré-requisitos

1. **Claude Code instalado** e no `PATH` (`claude --version`).
2. **Token OAuth da assinatura.** Gere com:

   ```bash
   claude setup-token
   export CLAUDE_CODE_OAUTH_TOKEN="<token gerado>"
   ```

   Isso usa o limite da sua assinatura Pro/Max em vez de cobrar por token.
3. **Um Bearer token para a API** (qualquer string secreta forte):

   ```bash
   export CLAUDED_API_TOKEN="$(openssl rand -hex 32)"
   ```

---

## Configuração

Precedência (maior → menor): **flag de linha de comando → variável de ambiente
`CLAUDED_*` → arquivo YAML (`--config`) → default**.

| Env | Flag | Default | Descrição |
|---|---|---|---|
| `CLAUDED_ADDR` | `--addr` | `127.0.0.1:8787` | Bind do servidor |
| `CLAUDED_API_TOKEN` | — | (obrigatório) | Bearer token da API |
| `CLAUDE_CODE_OAUTH_TOKEN` | — | (obrigatório¹) | Token da assinatura (`claude setup-token`) |
| `ANTHROPIC_API_KEY` | — | — | Alternativa paga; necessária para `bare:true` |
| `CLAUDED_ALLOWED_ROOTS` | `--allowed-roots` | `$HOME/projects` | Raízes permitidas para `workdir` |
| `CLAUDED_MAX_CONCURRENCY` | `--max-concurrency` | `2` | Runs simultâneas |
| `CLAUDED_DEFAULT_MODEL` | `--default-model` | `sonnet` | Modelo padrão |
| `CLAUDED_CLAUDE_BIN` | `--claude-bin` | `claude` | Caminho do binário claude |
| `CLAUDED_RUN_TIMEOUT` | `--run-timeout` | `10m` | Timeout por run |
| `CLAUDED_ALLOW_DANGEROUS` | — | `false` | Habilita `bypassPermissions`/`dontAsk` |
| `CLAUDED_LOG_FORMAT` | `--log-format` | `json` | `json` ou `text` |
| `CLAUDED_LOG_LEVEL` | `--log-level` | `info` | `debug`/`info`/`warn`/`error` |
| `CLAUDED_SESSION_STORE` | `--session-store` | `$HOME/.clauded/sessions.json` | Store de sessões |
| `CLAUDED_RATE_LIMIT_PER_MINUTE` | `--rate-limit-per-minute` | `60` | Requisições/cliente/min (0=ilimitado) |
| `CLAUDED_METRICS_ENABLED` | `--metrics` | `false` | Expõe `/metrics` |
| `CLAUDED_CONFIG` | `--config` | — | Caminho do arquivo YAML |

¹ É obrigatório ter `CLAUDE_CODE_OAUTH_TOKEN` **ou** `ANTHROPIC_API_KEY`.

Veja [`clauded.example.yaml`](clauded.example.yaml) para um arquivo comentado.

Subindo o serviço:

```bash
export CLAUDED_API_TOKEN="..."
export CLAUDE_CODE_OAUTH_TOKEN="..."
clauded --allowed-roots "$HOME/projects" --log-format text
# → INFO clauded iniciado addr=127.0.0.1:8787 ...
```

---

## Uso

Todos os endpoints (exceto `/healthz`) exigem `Authorization: Bearer $CLAUDED_API_TOKEN`.

```bash
# Run simples (JSON)
curl -sS https://clauded.seudominio.com/v1/runs \
  -H "Authorization: Bearer $CLAUDED_API_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"prompt":"Refatore o pacote auth para usar JWT","workdir":"/Users/me/projects/api","model":"sonnet"}'

# Streaming (SSE)
curl -N https://clauded.seudominio.com/v1/runs \
  -H "Authorization: Bearer $CLAUDED_API_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"prompt":"Explique a arquitetura","workdir":"/Users/me/projects/api","stream":true}'

# Retomar uma sessão (mesmo workdir!)
curl -sS https://clauded.seudominio.com/v1/runs \
  -H "Authorization: Bearer $CLAUDED_API_TOKEN" \
  -d '{"prompt":"Agora implemente o que sugeriu","resume":"<session-id>","workdir":"/Users/me/projects/api"}'

# Invocar um SKILL explicitamente (slash command no prompt)
curl -sS https://clauded.seudominio.com/v1/runs \
  -H "Authorization: Bearer $CLAUDED_API_TOKEN" \
  -d '{"prompt":"/security-review","workdir":"/Users/me/projects/api","permission_mode":"acceptEdits"}'

# Invocar um SKILL por linguagem natural (o modelo escolhe o que casa)
curl -sS https://clauded.seudominio.com/v1/runs \
  -H "Authorization: Bearer $CLAUDED_API_TOKEN" \
  -d '{"prompt":"gere um relatório PDF a partir do README deste projeto","workdir":"/Users/me/projects/api","tools":"Read,Bash,Write"}'

# Listar sessões
curl -sS https://clauded.seudominio.com/v1/sessions \
  -H "Authorization: Bearer $CLAUDED_API_TOKEN"
```

### Sessões: resume, continue, fork

O Claude Code grava o histórico em
`~/.claude/projects/<workdir-codificado>/<session-id>.jsonl`. **Retomar só
funciona com o mesmo `workdir`** — o `clauded` amarra cada `session_id` ao seu
`workdir` no store e o reusa automaticamente (você pode até omitir `workdir` num
`resume` de sessão conhecida).

- `resume`: retoma uma sessão específica (recomendado para a API).
- `continue`: retoma a sessão mais recente do `workdir`.
- `fork`: cria uma nova sessão a partir do histórico, sem alterar a original.

Se uma run parar por limite (`subtype: error_max_turns` ou
`error_max_budget_usd`), o `session_id` é devolvido para você retomar com um
limite maior.

### Skills e slash commands

Skills funcionam de duas formas: explicitamente (`"prompt": "/security-review"`)
ou por linguagem natural (o modelo escolhe o skill cujo `description` casa). O
skill precisa estar **instalado na máquina** onde o `claude` roda. Comandos
interativos (`/login`, `/config`) não funcionam em modo headless. Skills que
usam ferramentas dependem de `permission_mode`/`tools`.

---

## Referência da API

Especificação completa em [`api/openapi.yaml`](api/openapi.yaml) (OpenAPI 3.1).

**Endpoints:** `POST /v1/runs`, `GET /v1/sessions`, `GET /v1/sessions/{id}`,
`GET /healthz` (público), `GET /readyz`, `GET /version`.

Principais campos do corpo de `POST /v1/runs` (mapeiam para flags do `claude -p`):

| Campo | Flag | Notas |
|---|---|---|
| `prompt` *(obrigatório)* | `-p` | Pode iniciar com `/<skill>` |
| `workdir` | `--add-dir` + `cwd` | Deve estar na allowlist |
| `model` | `--model` | `sonnet`/`opus`/`haiku`/`fable` ou ID |
| `session_id` | `--session-id` | UUID v4 (gerado se ausente) |
| `resume` | `--resume` | UUID de sessão existente |
| `continue` | `--continue` | Sessão mais recente do workdir |
| `fork` | `--fork-session` | Novo ID ao retomar |
| `output_format` | `--output-format` | `text`/`json`/`stream-json` |
| `stream` | (SSE) | Força `stream-json` |
| `permission_mode` | `--permission-mode` | `default`/`acceptEdits`/`plan`/`auto`/`dontAsk`²/`bypassPermissions`² |
| `tools` | `--tools` | Ex.: `"Bash,Edit,Read"` |
| `max_turns` | `--max-turns` | Ver nota de compatibilidade abaixo |
| `max_budget_usd` | `--max-budget-usd` | Teto de gasto |
| `append_system_prompt` / `system_prompt` | idem | |
| `mcp_config` / `strict_mcp_config` | idem | |
| `agents` / `json_schema` | idem | |
| `effort` | `--effort` | `low`/`medium`/`high`/`xhigh` |
| `fallback_model` | `--fallback-model` | |
| `setting_sources` | `--setting-sources` | Ex.: `"user,project,local"` |
| `plugin_dirs` / `plugin_urls` | `--plugin-dir`/`--plugin-url` | `plugin_dirs` validado contra a allowlist |
| `bare` | `--bare` | ⚠️ Ver nota abaixo |

² Exigem `CLAUDED_ALLOW_DANGEROUS=true`, senão `403`.

> **Nota sobre `bare`.** A flag `--bare` desativa a leitura de credenciais OAuth
> e do keychain — nesse modo o Claude Code só autentica com `ANTHROPIC_API_KEY`.
> Portanto `bare:true` é **incompatível com a assinatura** e o `clauded` o
> rejeita com `400` a menos que `ANTHROPIC_API_KEY` esteja configurada.

> **Nota sobre `max_turns`.** O flag `--max-turns` pode não existir em todas as
> versões do CLI `claude`. O `clauded` o envia quando você o especifica; se a
> sua versão não o reconhecer, a run falhará com erro do CLI. Verifique com o
> teste de integração (`make test-integration`).

---

## Acesso externo via Cloudflare Tunnel

O Cloudflare Tunnel faz conexão **saída-apenas** para a borda da Cloudflare —
não exige abrir portas no firewall/roteador, e a Cloudflare provê TLS no seu
subdomínio. Requer um domínio gerenciado pela Cloudflare.

```bash
# 1. Instale o cloudflared
./scripts/install-cloudflared.sh

# 2. Autentique na conta
cloudflared tunnel login

# 3. Crie o túnel (gera credenciais + Tunnel ID)
cloudflared tunnel create clauded

# 4. Roteie um hostname
cloudflared tunnel route dns clauded clauded.seudominio.com

# 5. Configure (veja deploy/cloudflared-config.yml)
cp deploy/cloudflared-config.yml ~/.cloudflared/config.yml
# edite <TUNNEL_ID> e o caminho das credenciais

# 6. Rode
cloudflared tunnel run clauded
```

**Defesa em profundidade:** mesmo com o túnel, mantenha o Bearer token e,
idealmente, ative o **Cloudflare Access** (políticas de identidade na borda) na
frente do hostname, como segunda camada de autenticação.

### Alternativa rápida (protótipo): ngrok

```bash
ngrok http 8787
```

Mais simples e efêmero, porém **menos seguro** (URL pública aleatória). O Bearer
token continua obrigatório.

---

## Rodar como serviço

### Linux (systemd)

```bash
cp deploy/clauded.service ~/.config/systemd/user/
# crie ~/.config/clauded.env (chmod 600) com os segredos e ~/.config/clauded.yaml
systemctl --user daemon-reload
systemctl --user enable --now clauded
loginctl enable-linger "$USER"   # mantém rodando após logout
```

### macOS (launchd)

```bash
cp deploy/com.user.clauded.plist ~/Library/LaunchAgents/
# edite os caminhos absolutos e os tokens no arquivo
launchctl load -w ~/Library/LaunchAgents/com.user.clauded.plist
```

Veja os comentários em cada arquivo de `deploy/` para detalhes e hardening.

---

## Desenvolvimento

```bash
make build              # compila para dist/clauded
make test               # testes unitários (-race -cover)
make test-integration   # invoca o claude real (requer token)
make lint               # golangci-lint
make cross              # cross-compila os 5 alvos manualmente
make release            # goreleaser (em tag)
```

Layout do repositório:

```
cmd/clauded/        # main: flags, wire-up, graceful shutdown
internal/config/    # config em 3 camadas
internal/runner/    # tradução RunRequest -> argv, exec, parse
internal/server/    # mux, handlers, middlewares, SSE
internal/session/   # store JSON session_id -> workdir
internal/version/   # versão injetada via -ldflags
api/openapi.yaml    # spec da API
deploy/             # systemd, launchd, cloudflared
```

A tradução `RunRequest → argv` (`internal/runner/options.go`) é uma função pura,
coberta por testes de tabela; o executor é abstraído por uma interface com fake
para testar sem o binário real.

---

## FAQ / Troubleshooting

**`resume` voltou uma sessão vazia / sem contexto.** O `workdir` precisa ser o
**mesmo** de quando a sessão foi criada — o histórico do Claude Code é indexado
pelo `cwd`. O `clauded` cuida disso pelo store, mas se você passou um `workdir`
diferente do original, será uma sessão nova.

**`401 Unauthorized`.** Falta o header `Authorization: Bearer <token>` ou o
token não bate com `CLAUDED_API_TOKEN`.

**`403 workdir_forbidden`.** O `workdir` está fora de `CLAUDED_ALLOWED_ROOTS`.
Ajuste as raízes permitidas.

**`readyz` retorna 503 / "claude não encontrado".** O binário `claude` não está
no `PATH` do processo `clauded`. Defina `CLAUDED_CLAUDE_BIN` com o caminho
absoluto ou ajuste o `PATH` do serviço.

**`bare_requires_api_key`.** Você passou `bare:true` sem `ANTHROPIC_API_KEY` —
veja a nota sobre `bare` acima.

**A run falha com erro mencionando `--max-turns`.** Sua versão do `claude` pode
não suportar esse flag; remova `max_turns` da requisição.

---

## Licença

[MIT](LICENSE) © 2026 Addo Del Grossi.
