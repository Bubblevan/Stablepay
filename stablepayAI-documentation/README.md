# StablePay Developer Documentation

Documentation site for [StablePay](https://github.com/stablepay) — decentralized payments for AI Agents. Built with [Mintlify](https://mintlify.com).

## Local development

```bash
npm i -g mint
mint dev
```

Preview at `http://localhost:3000`.

## Structure

```
docs.json              # Site config, navigation, branding
index.mdx              # Landing page
quickstart.mdx         # 5-minute getting started
development.mdx        # Plugin configuration reference
wallet-setup.mdx       # Wallet creation & binding
did-registration.mdx   # DID registration
integrating-payment.mdx # x402 payment integration
x402-format.mdx        # 402 response format spec
moltbay.mdx            # MoltBay marketplace
revenue.mdx            # Revenue & sales querying
api-reference/         # REST API docs (OpenAPI-driven)
```

## Deployment

Push to `main` — Mintlify GitHub App auto-deploys.

## Resources

- [Mintlify documentation](https://mintlify.com/docs)
- [StablePay plugin](https://github.com/stablepay/openclaw-plugin-tryon)
- [MoltBay](https://www.moltbay.com)
