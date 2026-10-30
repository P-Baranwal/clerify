# AI providers

There is one client with two wire formats, so any key works. Presets only
pre-fill the base URL, auth header, and default model. The default provider is
OpenAI; run `clerify init` to choose another one.

| Preset | Endpoint | Auth |
|---|---|---|
| `openai` | `https://api.openai.com/v1` | `Authorization: Bearer` |
| `anthropic` | `https://api.anthropic.com/v1` | `x-api-key`, `wire: anthropic` |
| `gemini` | `https://generativelanguage.googleapis.com/v1beta/openai` | `Authorization: Bearer` |
| `groq` | `https://api.groq.com/openai/v1` | `Authorization: Bearer` |
| `openrouter` | `https://openrouter.ai/api/v1` | `Authorization: Bearer` |
| `together` | `https://api.together.xyz/v1` | `Authorization: Bearer` |
| `deepseek` | `https://api.deepseek.com/v1` | `Authorization: Bearer` |
| `mistral` | `https://api.mistral.ai/v1` | `Authorization: Bearer` |
| `xai` | `https://api.x.ai/v1` | `Authorization: Bearer` |
| `sarvam` | `https://api.sarvam.ai/v1` | `api-subscription-key` |
| `ollama` | `http://localhost:11434/v1` | ignored |
| `lmstudio` | `http://localhost:1234/v1` | ignored |
| `llamacpp` | `http://localhost:8080/v1` | ignored |
| `vllm` | `http://localhost:8000/v1` | ignored |
| `custom` | you supply | you supply |

## Keys

Keys are resolved in this order:

1. `--api-key`
2. `CLERIFY_API_KEY`
3. `<PRESET>_API_KEY` (for example `OPENAI_API_KEY`)
4. `config.json`

The four local presets (`ollama`, `lmstudio`, `llamacpp`, `vllm`) set
`no_auth`, so they work without a key and send no authentication header.

Switching presets clears the previous provider's key, so an OpenAI key is never
sent to DeepSeek's endpoint. Provider-specific body fields such as Sarvam's
`reasoning_effort` are applied only by that preset and can be removed by
editing `config.json`.

## Custom endpoints

Anything OpenAI-compatible works without a preset:

```sh
clerify ai preset custom --base-url http://localhost:11434/v1 --model qwen2.5-coder:7b
clerify ai preset custom --base-url https://my-gateway.example/v1 --header x-api-key --prefix "" --wire openai
```

Provider quirks (extra body fields, extra headers, JSON mode) are configurable:

```sh
$EDITOR ~/.config/clerify/config.json   # ai.extra_body, ai.extra_headers, ai.response_format_json
```

## Checking the setup

```sh
clerify ai show      # effective config, key masked
clerify ai test      # one tiny request and latency
clerify ai models    # when the provider exposes /models
clerify doctor       # local setup, key presence, endpoint
```
