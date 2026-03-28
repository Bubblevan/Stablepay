const navItems = [
  { label: 'Marketplace', href: '#skills' },
  { label: 'Features', href: '#features' },
  { label: 'How It Works', href: '#how-it-works' },
  { label: 'Protocols', href: '#protocols' },
  { label: 'Developers', href: '#developers' },
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
  return (
    <header className="site-header">
      <div className="container header-inner">
        <a className="brand" href="#top" aria-label="StablePay home">
          <span className="brand-badge">S</span>
          <span>
            <strong>StablePay</strong>
            <small>AI Payments on Solana</small>
          </span>
        </a>
        <nav className="nav">
          {navItems.map((item) => (
            <a key={item.href} href={item.href}>
              {item.label}
            </a>
          ))}
        </nav>
      </div>
    </header>
  )
}

function Hero() {
  return (
    <section className="hero" id="top">
      <div className="container hero-grid">
        <div>
          <div className="eyebrow">Stablecoin checkout for the AI skill economy</div>
          <h1>Let your Agent buy premium skills — without breaking the flow.</h1>
          <p className="hero-copy">
            StablePay gives AI agents a wallet-backed identity, a clean HTTP 402 payment flow,
            and a developer-friendly paywall template. Inspired by the protocol storytelling of
            MoltBay, but focused on Solana payments, skill monetization, and ClawHub/OpenClaw
            demo scenarios.
          </p>
          <div className="hero-actions">
            <a className="button primary" href="#developers">
              View Code Template
            </a>
            <a className="button secondary" href="#how-it-works">
              See Demo Flow
            </a>
          </div>
          <div className="hero-stats">
            <div>
              <strong>402</strong>
              <span>payment-first UX</span>
            </div>
            <div>
              <strong>Solana</strong>
              <span>USDC / USDT focus</span>
            </div>
            <div>
              <strong>5 min</strong>
              <span>developer integration target</span>
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
  return (
    <section className="audience section">
      <div className="container two-up">
        <article className="panel audience-card">
          <span className="pill">For Agent Users</span>
          <h3>Buy skills in conversation</h3>
          <p>
            Create a wallet, bind X, top up USDC, and let your agent auto-buy low-cost skills or
            ask for confirmation on higher-value tasks.
          </p>
          <ul>
            <li>Wallet + DID onboarding</li>
            <li>Auto-buy threshold</li>
            <li>Balance and transaction history</li>
          </ul>
        </article>
        <article className="panel audience-card">
          <span className="pill alt">For Skill Developers</span>
          <h3>Monetize with a simple template</h3>
          <p>
            Copy the payment snippet, replace your skill DID and price, and optionally verify
            purchases on the backend before executing premium actions.
          </p>
          <ul>
            <li>Copy-paste payment template</li>
            <li>Revenue and sales placeholders</li>
            <li>Optional verify API integration</li>
          </ul>
        </article>
      </div>
    </section>
  )
}

function Skills() {
  return (
    <section className="section" id="skills">
      <div className="container">
        <div className="section-heading">
          <div className="eyebrow">Demo marketplace</div>
          <h2>Placeholder skills your agent could discover and buy</h2>
          <p>
            These cards are intentionally mock data. Keep them as placeholders for now, or replace
            them with your real skills later.
          </p>
        </div>
        <div className="skills-grid">
          {skills.map((skill) => (
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
                <span>Starting at {skill.price}</span>
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
  return (
    <section className="section" id="features">
      <div className="container">
        <div className="section-heading narrow">
          <div className="eyebrow">Core features</div>
          <h2>Your payment layer for agent commerce</h2>
        </div>
        <div className="features-grid">
          {features.map((feature) => (
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
  return (
    <section className="section" id="how-it-works">
      <div className="container">
        <div className="section-heading">
          <div className="eyebrow">How it works</div>
          <h2>StablePay in four demo steps</h2>
        </div>
        <div className="steps-grid">
          {steps.map((step) => (
            <article className="panel step-card" key={step.number}>
              <div className="step-number">{step.number}</div>
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
  return (
    <section className="section" id="developers">
      <div className="container split-layout">
        <div>
          <div className="eyebrow">Developer zone</div>
          <h2>Copy the template, replace placeholders, publish a paid skill</h2>
          <p>
            This block is designed as the page anchor you can demo live. It matches your product
            direction: no dashboard, no login wall, just a clear template developers can paste into
            their skill docs.
          </p>
          <div className="api-list panel">
            <h3>Placeholder API surface</h3>
            <ul>
              {apiItems.map((item) => (
                <li key={item}>{item}</li>
              ))}
            </ul>
          </div>
        </div>

        <div className="panel code-panel">
          <div className="code-header">
            <span>skill.md</span>
            <button type="button">Copy Template</button>
          </div>
          <pre>{`## 💰 StablePay Payment

This Skill requires {PRICE} USDC to unlock.

Payment endpoint:
https://api.stablepay.co/pay?skill={SKILL_DID}&price={PRICE}

Verify purchase:
https://api.stablepay.co/verify?skill={SKILL_DID}&agent={AGENT_DID}

Recommended flow:
1. Return HTTP 402 for unpaid access
2. Let StablePay handle signing + payment
3. Re-run the original request after purchase`}</pre>
        </div>
      </div>
    </section>
  )
}

function Protocols() {
  return (
    <section className="section" id="protocols">
      <div className="container two-up">
        <article className="panel protocol-card">
          <div className="eyebrow">Identity</div>
          <h3>did:solana</h3>
          <p>
            Wallet-backed decentralized identifiers for users and developers, with local signing and
            clean ownership semantics.
          </p>
          <ul>
            <li>Wallet creation</li>
            <li>Signature verification</li>
            <li>X-bound trust layer</li>
          </ul>
        </article>
        <article className="panel protocol-card">
          <div className="eyebrow">Payments</div>
          <h3>HTTP 402 + Solana</h3>
          <p>
            A machine-friendly paywall that can be triggered automatically, settled in stablecoins,
            and optionally verified by developer backends.
          </p>
          <ul>
            <li>Programmable paywalls</li>
            <li>Instant settlement narrative</li>
            <li>Verification API story</li>
          </ul>
        </article>
      </div>
    </section>
  )
}

function FAQ() {
  return (
    <section className="section faq-section">
      <div className="container">
        <div className="section-heading narrow">
          <div className="eyebrow">FAQ</div>
          <h2>Demo notes</h2>
        </div>
        <div className="faq-list">
          {faqs.map((item) => (
            <details className="panel faq-item" key={item.q}>
              <summary>{item.q}</summary>
              <p>{item.a}</p>
            </details>
          ))}
        </div>
      </div>
    </section>
  )
}

function Footer() {
  return (
    <footer className="site-footer">
      <div className="container footer-inner">
        <div>
          <div className="brand footer-brand">
            <span className="brand-badge">S</span>
            <span>
              <strong>StablePay</strong>
              <small>Demo landing page</small>
            </span>
          </div>
          <p>
            Built for demo use. Replace placeholder copy, skills, and URLs when your backend is
            ready.
          </p>
        </div>
        <div className="footer-links">
          <a href="#developers">Template</a>
          <a href="#features">Features</a>
          <a href="#how-it-works">Flow</a>
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
        <Developers />
        <Protocols />
        <FAQ />
      </main>
      <Footer />
    </>
  )
}
