# Task 02 K6 Baseline Scripts

## Purpose

These scripts support Task 02 API Gateway baseline pressure testing in ACK through WSL.

They are intended for:

- `/healthz`
- `/readyz`
- `/api/v1/pay/require`
- `/verify`
- auth-protected routes once valid credentials are available

## Recommended Runtime

Run from WSL `Ubuntu-24.04`, not from the current Windows shell.

## Example Commands

### Wrapper Script

```bash
bash /mnt/d/MyLab/StablePay/value-test/backend/scripts/k6/run_api_baseline.sh \
  /healthz \
  200 \
  /mnt/d/MyLab/StablePay/value-test/backend/reports/task-02/healthz-summary.json \
  - \
  5 \
  10s \
  5
```

### Wrapper Script With Query File

```bash
bash /mnt/d/MyLab/StablePay/value-test/backend/scripts/k6/run_api_baseline.sh \
  /verify \
  200 \
  /mnt/d/MyLab/StablePay/value-test/backend/reports/task-02/verify-short-summary.json \
  @/mnt/d/MyLab/StablePay/value-test/backend/scripts/k6/queries/verify-short.txt \
  5 \
  10s \
  5
```

### Task 02 Matrix

```bash
bash /mnt/d/MyLab/StablePay/value-test/backend/scripts/k6/run_task02_matrix.sh
```

Optional overrides:

```bash
BASE_URL=https://ai.wenfu.cn RATE=20 DURATION=30s PREALLOCATED_VUS=20 \
  bash /mnt/d/MyLab/StablePay/value-test/backend/scripts/k6/run_task02_matrix.sh
```

The script writes:

- JSON summaries under `value-test/backend/reports/task-02`
- a Markdown summary table at `value-test/backend/reports/task-02/summary.md`

### Health

```bash
k6 run \
  -e BASE_URL=https://ai.wenfu.cn \
  -e ROUTE=/healthz \
  -e EXPECTED_STATUS=200 \
  -e RATE=20 \
  -e DURATION=30s \
  -e PREALLOCATED_VUS=20 \
  /mnt/d/MyLab/StablePay/value-test/backend/scripts/k6/api_baseline.js
```

### 402 Challenge

```bash
k6 run \
  -e BASE_URL=https://ai.wenfu.cn \
  -e ROUTE=/api/v1/pay/require \
  -e QUERY='skill_did=did:solana:testskill' \
  -e EXPECTED_STATUS=402 \
  -e RATE=20 \
  -e DURATION=30s \
  -e PREALLOCATED_VUS=20 \
  /mnt/d/MyLab/StablePay/value-test/backend/scripts/k6/api_baseline.js
```

### Short Verify

```bash
k6 run \
  -e BASE_URL=https://ai.wenfu.cn \
  -e ROUTE=/verify \
  -e QUERY='skill=did:solana:testskill&agent=did:solana:testagent' \
  -e EXPECTED_STATUS=200 \
  -e RATE=20 \
  -e DURATION=30s \
  -e PREALLOCATED_VUS=20 \
  /mnt/d/MyLab/StablePay/value-test/backend/scripts/k6/api_baseline.js
```

### API-Key Route

```bash
k6 run \
  -e BASE_URL=https://ai.wenfu.cn \
  -e ROUTE=/api/v1/verify \
  -e QUERY='skill_did=did:solana:testskill&agent_did=did:solana:testagent' \
  -e EXPECTED_STATUS=200 \
  -e API_KEY='stablepay-dev-key' \
  -e RATE=20 \
  -e DURATION=30s \
  -e PREALLOCATED_VUS=20 \
  /mnt/d/MyLab/StablePay/value-test/backend/scripts/k6/api_baseline.js
```

## Notes

- Task 02 proves API-layer latency and error rate only.
- It does not prove payment completion throughput or full settlement throughput.
- Use `EXPECTED_STATUS=402` for challenge routes.
