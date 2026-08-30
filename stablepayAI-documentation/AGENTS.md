# Documentation project instructions

## About this project

- This is the StablePay developer documentation site built on [Mintlify](https://mintlify.com)
- Pages are MDX files with YAML frontmatter
- Configuration lives in `docs.json`
- Navigation is defined in `docs.json` under `navigation.tabs`
- Run `mint dev` to preview locally at `http://localhost:3000`
- Run `mint broken-links` to check links

## Terminology

| Term | Meaning |
|------|---------|
| **DID** | Decentralized Identifier in `did:solana:<base58-pubkey>` format. Serves as both identity and wallet address. |
| **skill_did** | For developers, this IS their DID. Payment recipient = `skill_did` → Solana address. |
| **OWS** | Open Wallet Standard. Local wallet management and signing. Private keys never leave the user's machine. |
| **x402** | HTTP 402-based machine payment protocol. StablePay's payment layer. |
| **A2A** | Agent-to-Agent protocol for service discovery and task delegation. MoltBay's communication layer. |
| **MoltBay** | The Agent marketplace where developers list Gigs and buyers discover services. |
| **OpenClaw** | The AI agent runtime. The StablePay plugin runs inside OpenClaw. |
| **Gateway** | `ai.wenfu.cn` — the centralized API Gateway deployed on ACK Kubernetes. |
| **Fee Payer / Hotwallet** | Platform wallet that covers Solana gas fees. Users need only USDC. |
| **TUI** | Terminal UI — the `openclaw tui` conversational interface where all StablePay interactions happen. |
| **Gig** | A service listing on MoltBay (skills + pricing). Equivalent to a "skill listing." |

Use "you" not "the user", "agent" for the AI assistant, "plugin" for `stablepay-agentpay-dev`.

## Style preferences

- Use active voice and second person ("you")
- Keep sentences concise — one idea per sentence
- Use sentence case for headings
- Bold for UI elements: Click **Settings**, Run **stablepay_create_local_wallet**
- Code formatting for tool names, file names, commands, paths, config keys, and code
- Use `>` blockquote style for example TUI conversation prompts
- Tool names always in code font: `stablepay_runtime_status`
- DID format: `did:solana:AbCdEf...` (truncated with ... for readability)
- USDC amounts: string decimal format (`"1.00"`) in API, minor units (`"1000000"`) in x402

## Content boundaries

Document: plugin usage, wallet setup, DID registration, x402 payment integration, MoltBay publishing, revenue querying, API reference.

Do NOT document: internal backend implementation details (service internals, k8s deployment, database schemas), Admin/ops procedures, Docker Compose setup (that's for backend devs).
