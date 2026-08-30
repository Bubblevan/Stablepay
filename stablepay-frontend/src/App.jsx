import { useState } from 'react'
import { useLanguage } from './LanguageContext'
import { translations } from './translations'

const navItems = [
  { key: 'forAIUsers', href: '#quickstart' },
  { key: 'walletGuide', href: '#wallet-guide' },
  { key: 'forAIDevelopers', href: 'https://ai.wenfu.cn/docs/', external: true },
]

const skills = [
  {
    icon: '✍️',
    name: 'Writing Copilot',
    handle: '@writer_agent',
    title: 'Long-form articles, briefs, and launch copy',
    tags: ['copywriting', 'blog', 'content'],
    price: '$1.00',
    metric: '3.2k installs',
  },
  {
    icon: '📊',
    name: 'Data Scout',
    handle: '@data_scout',
    title: 'Charts, dashboards, and lightweight reports',
    tags: ['analysis', 'visualization', 'reports'],
    price: '$2.00',
    metric: '2.4k installs',
  },
  {
    icon: '🧠',
    name: 'Research Pilot',
    handle: '@research_pilot',
    title: 'Fast research summaries with source links',
    tags: ['research', 'summary', 'sources'],
    price: '$3.00',
    metric: '1.6k installs',
  },
  {
    icon: '🎨',
    name: 'Design Draft',
    handle: '@design_draft',
    title: 'Simple logos, banners, and product visuals',
    tags: ['branding', 'visuals', 'design'],
    price: '$5.00',
    metric: '1.1k installs',
  },
]

const features = [
  {
    title: 'did:solana identity',
    text: 'Create a wallet-backed DID for every user or developer and keep the signing key local.',
  },
  {
    title: 'HTTP 402 payments',
    text: 'Trigger programmable paywalls for AI skills with a standard machine-friendly payment flow.',
  },
  {
    title: 'X verification + reward',
    text: 'Bind an X account, reduce abuse, and demonstrate a registration reward flow in the product story.',
  },
  {
    title: 'Fast integration',
    text: 'Developers copy a template, replace the DID and price, and publish a paid skill in minutes.',
  },
  {
    title: 'Developer verification API',
    text: 'Backends can verify purchases before executing premium actions so the paywall is harder to bypass.',
  },
  {
    title: 'Agent-native UX',
    text: 'Low-ticket skills auto-buy, higher amounts request confirmation, and results stay conversational.',
  },
]

const steps = [
  {
    number: '01',
    title: 'Create a wallet DID',
    text: 'StablePay creates a Solana wallet and a did:solana identity for your agent or developer profile.',
    code: 'did:solana:4fK9x2Hy...',
  },
  {
    number: '02',
    title: 'Verify with X',
    text: 'Post a verification tweet, paste the URL, and claim a small reward to prove ownership and reduce spam.',
    code: 'Verify & Claim',
  },
  {
    number: '03',
    title: 'Pay via HTTP 402',
    text: 'When a premium skill returns Payment Required, StablePay signs and completes the purchase flow.',
    code: '402 -> signed pay -> 200',
  },
  {
    number: '04',
    title: 'Deliver results',
    text: 'The skill executes, developers get paid, and the user sees the updated balance and history.',
    code: 'balance: 47 USDC',
  },
]

const apiItems = [
  'GET /verify?skill={SKILL_DID}&agent={AGENT_DID}',
  'GET /balance?agent={AGENT_DID}',
  'GET /transactions?agent={AGENT_DID}&limit=10',
  'GET /revenue?skill={SKILL_DID}',
  'GET /sales?skill={SKILL_DID}&limit=10',
  'POST /verify-twitter',
]

const faqs = [
  {
    q: 'Is this page production-ready?',
    a: 'It is a polished demo landing page. The API calls and skill cards are placeholders that can later be wired to your real backend.',
  },
  {
    q: 'Why does the page have a MoltBay-like structure?',
    a: 'You asked for a reference from moltbay.com, so this demo mirrors its dark, protocol-first marketing style while rewriting the story for StablePay.',
  },
  {
    q: 'What can I change first?',
    a: 'Replace the placeholder skills, swap demo URLs with real endpoints, and update the code template card with your final payment contract or API.',
  },
]

function Header() {
  const { language, toggleLanguage } = useLanguage()
  const t = translations[language]

  return (
    <header className="site-header">
      <div className="container header-inner">
        <a className="brand" href="#top" aria-label="StablePay home">
          <img src="/logo.svg" alt="StablePay" className="brand-logo" />
        </a>
        <nav className="nav">
          {navItems.map((item) => (
            <a
              key={item.key}
              href={item.href}
              {...(item.external ? { target: '_blank', rel: 'noopener noreferrer' } : {})}
            >
              {t.nav[item.key]}
            </a>
          ))}
        </nav>
        <button
          className="language-toggle"
          onClick={toggleLanguage}
          aria-label={`Switch to ${language === 'en' ? 'Chinese' : 'English'}`}
        >
          {language === 'en' ? '中文' : 'EN'}
        </button>
      </div>
    </header>
  )
}

function Hero() {
  const { language } = useLanguage()
  const t = translations[language]

  return (
    <section className="hero" id="top">
      <div className="container hero-grid">
        <div>
          <div className="eyebrow">{t.hero.eyebrow}</div>
          <h1>{t.hero.title}</h1>
          <p className="hero-copy">
            {t.hero.copy}
          </p>
          <div className="hero-actions">
            <a className="button primary" href="#developers">
              {t.hero.viewCode}
            </a>
            <a className="button secondary" href="#how-it-works">
              {t.hero.seeDemo}
            </a>
          </div>
          <div className="hero-stats">
            <div>
              <strong>402</strong>
              <span>{t.hero.stats.payment}</span>
            </div>
            <div>
              <strong>Solana</strong>
              <span>{t.hero.stats.solana}</span>
            </div>
            <div>
              <strong>5 min</strong>
              <span>{t.hero.stats.integration}</span>
            </div>
          </div>
        </div>

        <div className="hero-panel panel">
          <div className="terminal-header">
            <span />
            <span />
            <span />
          </div>
          <div className="terminal-body">
            <div className="terminal-label">agent log</div>
            <pre>{`> install stablepay skill
✅ wallet created
DID: did:solana:4fK9x2Hy...
Reward: 1 USDC

> install AI Writing Assistant
402 Payment Required
price: 3 USDC
threshold: 5 USDC

✅ auto-purchase complete
balance: 47 USDC`}</pre>
          </div>
        </div>
      </div>
    </section>
  )
}

function Audience() {
  const { language } = useLanguage()
  const t = translations[language]

  return (
    <section className="audience section">
      <div className="container two-up">
        <article className="panel audience-card">
          <span className="pill">{t.audience.forUsers}</span>
          <h3>{t.audience.userTitle}</h3>
          <p>
            {t.audience.userDesc}
          </p>
          <ul>
            {t.audience.userList.map((item, index) => (
              <li key={index}>{item}</li>
            ))}
          </ul>
        </article>
        <article className="panel audience-card">
          <span className="pill alt">{t.audience.forDevelopers}</span>
          <h3>{t.audience.devTitle}</h3>
          <p>
            {t.audience.devDesc}
          </p>
          <ul>
            {t.audience.devList.map((item, index) => (
              <li key={index}>{item}</li>
            ))}
          </ul>
        </article>
      </div>
    </section>
  )
}

function Skills() {
  const { language } = useLanguage()
  const t = translations[language]

  return (
    <section className="section" id="skills">
      <div className="container">
        <div className="section-heading">
          <div>
            <div className="eyebrow">{t.skills.eyebrow}</div>
            <h2>{t.skills.title}</h2>
          </div>
          <p>{t.skills.desc}</p>
        </div>
        <div className="skills-grid">
          {t.skills.items.map((skill) => (
            <article className="panel skill-card" key={skill.name}>
              <div className="skill-topline">
                <span className="skill-icon">{skill.icon}</span>
                <div>
                  <div className="skill-name">{skill.name}</div>
                  <div className="skill-handle">{skill.handle}</div>
                </div>
              </div>
              <h3>{skill.title}</h3>
              <div className="tag-row">
                {skill.tags.map((tag) => (
                  <span className="tag" key={tag}>
                    {tag}
                  </span>
                ))}
              </div>
              <div className="skill-footer">
                <span>{t.skills.startingAt} {skill.price}</span>
                <span>{skill.metric}</span>
              </div>
            </article>
          ))}
        </div>
      </div>
    </section>
  )
}

function Features() {
  const { language } = useLanguage()
  const t = translations[language]

  return (
    <section className="section" id="features">
      <div className="container">
        <div className="section-heading narrow">
          <div className="eyebrow">{t.features.eyebrow}</div>
          <h2>{t.features.title}</h2>
        </div>
        <div className="features-grid">
          {t.features.items.map((feature) => (
            <article className="panel feature-card" key={feature.title}>
              <h3>{feature.title}</h3>
              <p>{feature.text}</p>
            </article>
          ))}
        </div>
      </div>
    </section>
  )
}

function HowItWorks() {
  const { language } = useLanguage()
  const t = translations[language]

  return (
    <section className="section" id="how-it-works">
      <div className="container">
        <div className="section-heading narrow">
          <div className="eyebrow">{t.howItWorks.eyebrow}</div>
          <h2>{t.howItWorks.title}</h2>
        </div>
        <div className="steps-grid">
          {t.steps.map((step, index) => (
            <article className="panel step-card" key={step.number || index + 1}>
              <div className="step-number">{String(index + 1).padStart(2, '0')}</div>
              <h3>{step.title}</h3>
              <p>{step.text}</p>
              <code>{step.code}</code>
            </article>
          ))}
        </div>
      </div>
    </section>
  )
}

function Developers() {
  const { language } = useLanguage()
  const t = translations[language]
  const [copied, setCopied] = useState(false)

  const copyTemplate = async () => {
    const template = `---
name: {{SKILL_NAME}}
description: {{DESCRIPTION}}
---

# {{SKILL_NAME}}

{{DESCRIPTION}}

## Merchant configuration

- skill_name: \`{{SKILL_NAME}}\`
- skill_did: \`{{SKILL_DID}}\`
- default_price_usdc: \`{{PRICE_USDC}}\`
- currency: \`USDC\`
- stablepay_gateway_base_url: \`https://ai.wenfu.cn\`
- merchant_backend_base_url: \`{{MERCHANT_BACKEND_BASE_URL}}\`
- verify_endpoint: \`https://ai.wenfu.cn/api/v1/verify\`
- premium_action_endpoint: \`{{PREMIUM_ACTION_ENDPOINT}}\`

## Protected premium workflow

When the user requests the premium capability:

1. Call the merchant backend premium action endpoint
2. If the backend returns 200, return the premium result
3. If the backend returns 402 Payment Required:
   - Parse x402 response from accepts[0]
   - Call stablepay_pay_via_gateway with extracted values:
     - skill_did from accepts[0].extra.skillDid
     - price from accepts[0].maxAmountRequired (divide by 1,000,000)
     - currency from accepts[0].extra.currency
     - facilitator_url from accepts[0].extra.facilitatorUrl
4. Retry the premium action after successful payment

## Verification rules

- Never treat local plugin state as proof of purchase
- Always rely on backend verification or confirmed StablePay purchase
- Never bypass merchant backend verification for protected actions`

    try {
      await navigator.clipboard.writeText(template)
      setCopied(true)
      setTimeout(() => setCopied(false), 2000)
    } catch (err) {
      console.error('Failed to copy template:', err)
    }
  }

  return (
    <section className="section" id="developers">
      <div className="container split-layout">
        <div>
          <div className="eyebrow">{t.developers.eyebrow}</div>
          <h2>{t.developers.title}</h2>
          <p>
            {t.developers.desc}
          </p>
          <div className="api-list panel">
            <h3>{t.developers.apiTitle}</h3>
            <ul>
              {apiItems.map((item) => (
                <li key={item}>{item}</li>
              ))}
            </ul>
          </div>
          <a href="https://ai.wenfu.cn/docs/" target="_blank" rel="noopener noreferrer" className="docs-link">
            {t.developers.viewDocs} →
          </a>
        </div>

        <div className="panel code-panel">
          <div className="code-header">
            <span>skill.md</span>
            <button type="button" onClick={copyTemplate}>{copied ? t.developers.copied : t.developers.copyTemplate}</button>
          </div>
          <pre>{`---
name: {{SKILL_NAME}}
description: {{DESCRIPTION}}
---

# {{SKILL_NAME}}

## Merchant configuration

- skill_did: {{SKILL_DID}}
- default_price_usdc: {{PRICE_USDC}}
- stablepay_gateway_base_url: https://ai.wenfu.cn
- verify_endpoint: https://ai.wenfu.cn/api/v1/verify`}</pre>
        </div>
      </div>
    </section>
  )
}

function Protocols() {
  const { language } = useLanguage()
  const t = translations[language]

  return (
    <section className="section" id="protocols">
      <div className="container two-up">
        <article className="panel protocol-card">
          <div className="eyebrow">{t.protocols.identity}</div>
          <h3>{t.protocols.didSolana}</h3>
          <p>
            {t.protocols.identityDesc}
          </p>
          <ul>
            {t.protocols.identityList.map((item, index) => (
              <li key={index}>{item}</li>
            ))}
          </ul>
        </article>
        <article className="panel protocol-card">
          <div className="eyebrow">{t.protocols.payments}</div>
          <h3>{t.protocols.http402}</h3>
          <p>
            {t.protocols.paymentsDesc}
          </p>
          <ul>
            {t.protocols.paymentsList.map((item, index) => (
              <li key={index}>{item}</li>
            ))}
          </ul>
        </article>
      </div>
    </section>
  )
}

function QuickStart() {
  const { language } = useLanguage()
  const t = translations[language]

  return (
    <section className="section" id="quickstart">
      <div className="container">
        <div className="section-heading narrow">
          <div className="eyebrow">{t.quickStart.eyebrow}</div>
          <h2>{t.quickStart.title}</h2>
        </div>
        {/* Step 0: Install OpenClaw */}
        <div className="quickstart-step">
          <h3>{t.quickStart.openclawInstall.title}</h3>
          <p>{t.quickStart.openclawInstall.desc}</p>

          <p><strong>{t.quickStart.openclawInstall.nodeReq}</strong></p>
          <div className="code-block">
            <pre>{t.quickStart.openclawInstall.nodeCmd}</pre>
          </div>

          <p><strong>{t.quickStart.openclawInstall.methodTitle}</strong></p>
          <p>{t.quickStart.openclawInstall.scriptOption}</p>
          <div className="code-block">
            <pre>{t.quickStart.openclawInstall.scriptCmd}</pre>
          </div>
          <p className="code-label">{t.quickStart.openclawInstall.scriptCn}</p>
          <div className="code-block">
            <pre>{t.quickStart.openclawInstall.scriptCnCmd}</pre>
          </div>
          <p>{t.quickStart.openclawInstall.npmOption}</p>
          <div className="code-block">
            <pre>{t.quickStart.openclawInstall.npmCmd}</pre>
          </div>

          <p><strong>{t.quickStart.openclawInstall.onboard}</strong></p>
          <div className="code-block">
            <pre>{t.quickStart.openclawInstall.onboardCmd}</pre>
          </div>

          <p><strong>{t.quickStart.openclawInstall.verify}</strong></p>
          <div className="code-block">
            <pre>{t.quickStart.openclawInstall.verifyCmd}</pre>
          </div>

          <div className="platform-notes">
            <p>{t.quickStart.openclawInstall.windows}</p>
            <p>{t.quickStart.openclawInstall.mac}</p>
            <p>{t.quickStart.openclawInstall.linux}</p>
          </div>
        </div>

        {/* Step 1: Install Plugin */}
        <div className="quickstart-step">
          <h3>{t.quickStart.install.title}</h3>
          <p>{t.quickStart.install.desc}</p>
          <div className="code-block">
            <pre>{`openclaw plugins install clawhub:stablepay-agentpay-dev@0.3.19 --force --dangerously-force-unsafe-install`}</pre>
          </div>
        </div>

        {/* Step 2: Chat to Initialize */}
        <div className="quickstart-step">
          <h3>{t.quickStart.wallet.title}</h3>
          <p>{t.quickStart.wallet.desc}</p>
        </div>

        {/* Step 3: Conversation Example */}
        <div className="quickstart-step">
          <h3>{t.quickStart.example.title}</h3>
          <p>{t.quickStart.example.desc}</p>
          <div className="conversation-example">
            <div className="chat-bubble user">
              <span className="chat-label">User:</span>
              <p>{t.quickStart.example.user1}</p>
            </div>
            <div className="chat-bubble agent">
              <span className="chat-label">OpenClaw:</span>
              <pre>{t.quickStart.example.agent1}</pre>
            </div>
            <div className="chat-bubble user">
              <span className="chat-label">User:</span>
              <p>{t.quickStart.example.user2}</p>
            </div>
            <div className="chat-bubble agent">
              <span className="chat-label">OpenClaw:</span>
              <pre>{t.quickStart.example.agent2}</pre>
            </div>
            <div className="chat-bubble user">
              <span className="chat-label">User:</span>
              <p>{t.quickStart.example.user3}</p>
            </div>
            <div className="chat-bubble agent">
              <span className="chat-label">OpenClaw:</span>
              <pre>{t.quickStart.example.agent3}</pre>
            </div>
            <div className="chat-bubble user">
              <span className="chat-label">User:</span>
              <p>{t.quickStart.example.user4}</p>
            </div>
            <div className="chat-bubble agent">
              <span className="chat-label">OpenClaw:</span>
              <pre>{t.quickStart.example.agent4}</pre>
            </div>
          </div>
        </div>

        {/* Step 3: Merchant Setup - Hidden for now
        <div className="quickstart-step">
          <h3>{t.quickStart.merchant.title}</h3>
          <p>{t.quickStart.merchant.desc}</p>
          <div className="code-block">
            <pre>{`git clone https://github.com/Bubblevan/showmethemoney-skills.git`}</pre>
          </div>
          <p>{t.quickStart.merchant.step1}</p>
          <div className="code-block">
            <pre>{`cp -r ./showmethemoney-skills/showmethemoney-pro ~/.openclaw/workspace/skills/`}</pre>
          </div>
          <p>{t.quickStart.merchant.step2}</p>
          <div className="code-block">
            <pre>{[
      'cd ./showmethemoney-skills/merchant-backend',
      'npm install',
      'npm run dev'
    ].join('\n')}</pre>
          </div>
          <p>{t.quickStart.merchant.step3}</p>
          <p>{t.quickStart.merchant.finalStep}</p>
          <p>{t.quickStart.merchant.note}</p>
        </div>
        */}
      </div>
    </section>
  )
}

function FAQ() {
  const { language } = useLanguage()
  const t = translations[language]

  return (
    <section className="section faq-section">
      <div className="container">
        <div className="faq-list">
          {t.faq.faqs.map((item, index) => (
            <details className="panel faq-item" key={index}>
              <summary>{item.q}</summary>
              <p>{item.a}</p>
            </details>
          ))}
        </div>
      </div>
    </section>
  )
}

function WalletGuide() {
  const { language } = useLanguage()
  const t = translations[language]

  return (
    <section className="section" id="wallet-guide">
      <div className="container">
        <div className="section-heading narrow">
          <div className="eyebrow">{t.walletGuide.eyebrow}</div>
          <h2>{t.walletGuide.title}</h2>
          <p>{t.walletGuide.subtitle}</p>
        </div>

        <div className="wallet-guide-grid">
          {/* USDC Card */}
          <div className="wallet-guide-card panel">
            <div className="guide-icon">💵</div>
            <h3>{t.walletGuide.usdc.title}</h3>
            <p>{t.walletGuide.usdc.desc}</p>
            <div className="guide-highlight">
              <strong>{t.walletGuide.usdc.highlight}</strong>
            </div>
          </div>

          {/* Wallet Card */}
          <div className="wallet-guide-card panel featured">
            <div className="guide-icon">👛</div>
            <h3>{t.walletGuide.wallet.title}</h3>
            <p>{t.walletGuide.wallet.desc}</p>
            <ul className="guide-list">
              {t.walletGuide.wallet.points.map((point, i) => (
                <li key={i}>{point}</li>
              ))}
            </ul>
            <div className="guide-address-example">
              <code>{t.walletGuide.wallet.example}</code>
            </div>
          </div>

          {/* DID Card */}
          <div className="wallet-guide-card panel">
            <div className="guide-icon">🆔</div>
            <h3>{t.walletGuide.did.title}</h3>
            <p>{t.walletGuide.did.desc}</p>
            <div className="guide-did-example">
              <code>{t.walletGuide.did.example}</code>
            </div>
          </div>

          {/* Signature Card */}
          <div className="wallet-guide-card panel">
            <div className="guide-icon">✍️</div>
            <h3>{t.walletGuide.signature.title}</h3>
            <p>{t.walletGuide.signature.desc}</p>
            <div className="guide-alert">
              <span>⚠️</span>
              <p>{t.walletGuide.signature.warning}</p>
            </div>
          </div>
        </div>
      </div>
    </section>
  )
}

function Footer() {
  const { language } = useLanguage()
  const t = translations[language]

  return (
    <footer className="site-footer">
      <div className="container footer-inner">
        <div>
          <div className="brand footer-brand">
            <span className="brand-badge">S</span>
            <span>
              <strong>StablePay</strong>
              <small>{t.footer.demo}</small>
            </span>
          </div>
          <p>
            {t.footer.desc}
          </p>
        </div>
        <div className="footer-links">
          <a href="#developers">{t.footer.template}</a>
          <a href="#features">{t.footer.features}</a>
          <a href="#how-it-works">{t.footer.flow}</a>
        </div>
      </div>
    </footer>
  )
}

export default function App() {
  return (
    <>
      <Header />
      <main>
        <Hero />
        <Audience />
        <Skills />
        <Features />
        <HowItWorks />
        <WalletGuide />
        <QuickStart />
        <FAQ />
        <Developers />
      </main>
      {/* <Footer /> */}
    </>
  )
}
