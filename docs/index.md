# 3270Web Documentation

3270Web is a web-based IBM 3270 terminal interface written in Go.

## What You Can Do

- Connect to 3270 hosts through a browser UI
- Record terminal interactions into `workflow.json`
- Replay workflows against sample apps or configured targets
- Configure terminal model, colors, fonts, and runtime `s3270` options

## Documentation

- [Configuration Reference](configuration.md)
- [Workflow Configuration](workflow.md)
- [Terminal Model Limits](terminal-model-limits.md)

## Local Docs Build

```bash
pip install -r requirements-docs.txt
mkdocs serve
```

Open `http://127.0.0.1:8000`.