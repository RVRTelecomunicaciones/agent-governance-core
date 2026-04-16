# Load harness — Phase 3.5.A baseline

Quickstart:

```bash
cd test/load
./runner.sh happy_path smoke
./runner.sh happy_path load
./runner.sh happy_path soak
./runner.sh dlq_flow     smoke
./runner.sh dlq_flow     load
./runner.sh dlq_flow     soak
./runner.sh breaker_flow smoke
./runner.sh breaker_flow load
./runner.sh breaker_flow soak
```

Results land in `results/<flow>-<intensity>-<timestamp>.json` and `.pg.txt`.

Ports used on host:
- `5433` → postgres
- `8081` → governance HTTP API

Depends on: Docker Desktop running, `k6` and `jq` installed via Homebrew.
