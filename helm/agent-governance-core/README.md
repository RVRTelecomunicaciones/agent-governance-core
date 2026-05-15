# agent-governance-core Helm chart

Decision layer for the Sophia ecosystem — routing, policy evaluation,
approvals, escalation, audit. Designed to run on an internal/VPC
network reachable only by the orchestator (no inbound auth in V1).

## Quick install

```bash
helm install governance ./helm/agent-governance-core \
  --set secrets.dbHost=pg-governance-core \
  --set secrets.dbUser=governance \
  --set secrets.dbPassword=$(vault kv get -field=password secret/sophia/governance) \
  --set secrets.dbName=governance \
  --set secrets.dbSslMode=require
```

## Production checklist

- [ ] `secrets.*` come from External Secrets Operator (Vault/AWS SM/GCP SM)
- [ ] `secrets.dbSslMode: require` (never `disable` outside dev)
- [ ] Run as `ClusterIP` service (NO public ingress) — V1 has no inbound auth
- [ ] If exposing publicly is unavoidable, front with mTLS gateway / API-key proxy
- [ ] Run goose migrations out-of-band via Job/CronJob (chart's `migrate.enabled` toggle is for prototype use)
- [ ] Pin `image.tag` to a digest

## Probes

- Liveness: `GET /health`
- Readiness: `GET /ready` (includes DB check)

## Network policies

Recommended NetworkPolicy: only the `sophia-orchestator` Pod selector
(plus operator bastion if any) can reach `:8080`. governance-core does
not have inbound auth in V1.

## See also

- `sophia-orchestator/helm/` — only client of governance-core in V1
- `agent-governance-core/migrations/postgres/` — append-only schema, goose format
