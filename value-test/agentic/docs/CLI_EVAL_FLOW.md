# StablePay CLI-First Eval Flow

This note explains how the agentic eval should be run when `npx` / CLI is the primary surface.

## 1. Principle

The evaluation entry point should be the StablePay CLI runtime, not the OpenClaw host.

That means:

- onboarding, doctor, pay, balance, and sales are treated as CLI-exposed capabilities
- the harness may still reuse plugin-internal tool definitions and runtime code
- but the product story is: `npx stablepay-agentpay-dev ...` or local `node ./dist/cli.cjs ...`

## 2. Which TASK first needs a real LLM?

### TASK-01 to TASK-05

No API key is required.

These tasks are contract and scaffolding work:

- failure taxonomy
- latency schema
- task schema
- fixture normalization
- final-state assertion contract

### TASK-06 and TASK-07

Still no API key is required to implement them.

Why:

- `TASK-06` is failure attribution wiring
- `TASK-07` is latency capture wiring

Both can be developed first in deterministic mode.

### First optional LLM call

The first *optional* real LLM smoke test can happen right after `TASK-07`, once the new output shape is wired.

Purpose:

- verify the new report fields are populated correctly in one or two live prompts
- catch obvious schema/report mismatches early

### First required LLM call

The first *required* real LLM run is effectively at `TASK-13`:

- run baseline
- generate a meaningful failure taxonomy histogram
- generate real latency breakdown

Without a live LLM at that point, you only have deterministic replay numbers, not resume-grade agent behavior numbers.

## 3. Do you need to configure API keys?

### For TASK-01 to TASK-07

No.

### For live LLM baseline or smoke tests

Yes, if we want the harness itself to call the model directly.

The current harness already supports:

- `OPENAI_API_KEY`
- `OPENAI_BASE_URL` (optional)
- `OPENAI_MODEL` (optional)
- `ANTHROPIC_AUTH_TOKEN` or `ANTHROPIC_API_KEY`
- `ANTHROPIC_BASE_URL` (optional)
- `ANTHROPIC_MODEL` (optional)

## 4. Recommended runtime split

### Local deterministic development

Use this for harness work:

```bash
cd /mnt/d/MyLab/StablePay/stablepay-openclaw-plugin
npm run eval:tooluse
```

### Local CLI validation

Use this to ensure the StablePay runtime itself is healthy:

```bash
cd /mnt/d/MyLab/StablePay/stablepay-openclaw-plugin
node ./dist/cli.cjs doctor
node ./dist/cli.cjs status
node ./dist/cli.cjs onboard --interactive
```

### Live LLM eval

Use this only after the new schemas and reporting shape are wired:

```bash
cd /mnt/d/MyLab/StablePay/stablepay-openclaw-plugin
OPENAI_API_KEY=... npm run eval:tooluse:llm
```

or:

```bash
cd /mnt/d/MyLab/StablePay/stablepay-openclaw-plugin
ANTHROPIC_AUTH_TOKEN=... npm run eval:tooluse -- --llm --provider anthropic
```

## 5. Do we need Claude Code to execute the eval?

No.

We do not need Claude Code as an external orchestrator for the main eval track.

Preferred order:

1. build the harness here
2. run deterministic and CLI checks here
3. run live LLM eval directly through the harness here

Claude Code is only useful if you later want:

- a separate external host comparison
- Claude-specific tool-use behavior comparison against OpenAI/Kimi

That is a later comparison layer, not a prerequisite.

## 6. Practical recommendation for you

Right now you do **not** need to configure an API key just to let me continue coding.

You **will** need one when we transition from:

- schema/harness wiring

to:

- real baseline measurement

If you want, I can continue straight into:

- `TASK-06` failure attribution wiring
- `TASK-07` latency/report wiring

and only ask you for an API key when we are ready to run the first live smoke eval.
