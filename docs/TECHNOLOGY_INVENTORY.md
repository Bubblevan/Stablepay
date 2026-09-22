# StablePay Technology Inventory

Only technologies evidenced by the frozen repository are listed here.

| Layer | Technology | Evidence / actual use |
|---|---|---|
| Language | Go | All Runtime and service implementations; `commerce-runtime/go.mod` and service `go.mod` files |
| HTTP | CloudWeGo Hertz | `api-gateway` and Merchant HTTP surfaces; service technical manual |
| RPC / IDL | CloudWeGo Kitex, Thrift, thriftgo | Six payment-plane services and `stablepayai-idl/idl/*.thrift` |
| Runtime API | Go HTTP server, JSON API, JSON-RPC MCP ingress | `commerce-runtime/internal/api/server.go` |
| Persistence | MySQL, GORM | Commerce Runtime repository and service repositories |
| Messaging | RocketMQ | Payment event path and local infrastructure compose |
| Payment protocol | x402 | Payment requirement facts, quote parsing, Merchant boundary |
| Chain | Solana / SPL asset adapters | `blockchain-adapter`, Devnet acceptance scripts, `solana-go` dependency |
| Agent boundary | MCP, CLI, authenticated HTTP | `stablepay.acquire`, `stablepay.status`, `stablepay.approve`; `cmd/stablepay-runtime` |
| Model transport | OpenAI-compatible LLM interface; DeepSeek-compatible configuration | `internal/llm`; real provider is opt-in and F0 is `NOT RUN` |
| Evaluation | JSONL traces, JSON metrics, Markdown report | `internal/eval`, `internal/observability`, `cmd/stablepay-agent-eval` |

Not claimed: Kubernetes production deployment, model training, RL/GRPO, dynamic workflow planning, parallel payment execution, QPS, or production availability.
