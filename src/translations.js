export const translations = {
  en: {
    nav: {
      marketplace: 'Marketplace',
      features: 'Features',
      howItWorks: 'How It Works',
      protocols: 'Protocols',
      developers: 'Developers'
    },
    hero: {
      eyebrow: 'Stablecoin checkout for the AI skill economy',
      title: 'Let your Agent buy premium skills — without breaking the flow.',
      copy: 'StablePay gives AI agents a wallet-backed identity, a clean HTTP 402 payment flow, and a developer-friendly paywall template. Inspired by the protocol storytelling of MoltBay, but focused on Solana payments, skill monetization, and ClawHub/OpenClaw demo scenarios.',
      viewCode: 'View Code Template',
      seeDemo: 'See Demo Flow',
      stats: {
        payment: 'payment-first UX',
        solana: 'USDC / USDT focus',
        integration: 'developer integration target'
      }
    },
    audience: {
      forUsers: 'For Agent Users',
      userTitle: 'Buy skills in conversation',
      userDesc: 'Create a wallet, bind X, top up USDC, and let your agent auto-buy low-cost skills or ask for confirmation on higher-value tasks.',
      userList: ['Wallet + DID onboarding', 'Auto-buy threshold', 'Balance and transaction history'],
      forDevelopers: 'For Skill Developers',
      devTitle: 'Monetize with a simple template',
      devDesc: 'Copy the payment snippet, replace your skill DID and price, and optionally verify purchases on the backend before executing premium actions.',
      devList: ['Copy-paste payment template', 'Revenue and sales placeholders', 'Optional verify API integration']
    },
    skills: {
      eyebrow: 'Demo marketplace',
      title: 'Placeholder skills your agent could discover and buy',
      desc: 'These cards are intentionally mock data. Keep them as placeholders for now, or replace them with your real skills later.',
      startingAt: 'Starting at',
      items: [
        { icon: '✍️', name: 'Writing Copilot', handle: '@writer_agent', title: 'Long-form articles, briefs, and launch copy', tags: ['copywriting', 'blog', 'content'], price: '$1.00', metric: '3.2k installs' },
        { icon: '📊', name: 'Data Scout', handle: '@data_scout', title: 'Charts, dashboards, and lightweight reports', tags: ['analysis', 'visualization', 'reports'], price: '$2.00', metric: '2.4k installs' },
        { icon: '🧠', name: 'Research Pilot', handle: '@research_pilot', title: 'Fast research summaries with source links', tags: ['research', 'summary', 'sources'], price: '$3.00', metric: '1.6k installs' },
        { icon: '🎨', name: 'Design Draft', handle: '@design_draft', title: 'Simple logos, banners, and product visuals', tags: ['branding', 'visuals', 'design'], price: '$5.00', metric: '1.1k installs' },
      ]
    },
    features: {
      eyebrow: 'Core features',
      title: 'Your payment layer for agent commerce',
      items: [
        { title: 'did:solana identity', text: 'Create a wallet-backed DID for every user or developer and keep the signing key local.' },
        { title: 'HTTP 402 payments', text: 'Trigger programmable paywalls for AI skills with a standard machine-friendly payment flow.' },
        { title: 'X verification + reward', text: 'Bind an X account, reduce abuse, and demonstrate a registration reward flow in the product story.' },
        { title: 'Fast integration', text: 'Developers copy a template, replace the DID and price, and publish a paid skill in minutes.' },
        { title: 'Developer verification API', text: 'Backends can verify purchases before executing premium actions so the paywall is harder to bypass.' },
        { title: 'Agent-native UX', text: 'Low-ticket skills auto-buy, higher amounts request confirmation, and results stay conversational.' },
      ]
    },
    howItWorks: {
      eyebrow: 'How it works',
      title: 'StablePay in four demo steps'
    },
    steps: [
      {
        title: 'Create a wallet DID',
        text: 'StablePay creates a Solana wallet and a did:solana identity for your agent or developer profile.',
        code: 'did:solana:4fK9x2Hy...'
      },
      {
        title: 'Verify with X',
        text: 'Post a verification tweet, paste the URL, and claim a small reward to prove ownership and reduce spam.',
        code: 'Verify & Claim'
      },
      {
        title: 'Pay via HTTP 402',
        text: 'When a premium skill returns Payment Required, StablePay signs and completes the purchase flow.',
        code: '402 -> signed pay -> 200'
      },
      {
        title: 'Deliver results',
        text: 'The skill executes, developers get paid, and the user sees the updated balance and history.',
        code: 'balance: 47 USDC'
      }
    ],
    developers: {
      eyebrow: 'Developer zone',
      title: 'Copy the template, replace placeholders, publish a paid skill',
      desc: 'This block is designed as the page anchor you can demo live. It matches your product direction: no dashboard, no login wall, just a clear template developers can paste into their skill docs.',
      apiTitle: 'Placeholder API surface',
      copyTemplate: 'Copy Template',
      copied: 'Copied!'
    },
    protocols: {
      identity: 'Identity',
      didSolana: 'did:solana',
      identityDesc: 'Wallet-backed decentralized identifiers for users and developers, with local signing and clean ownership semantics.',
      identityList: ['Wallet creation', 'Signature verification', 'X-bound trust layer'],
      payments: 'Payments',
      http402: 'HTTP 402 + Solana',
      paymentsDesc: 'A machine-friendly paywall that can be triggered automatically, settled in stablecoins, and optionally verified by developer backends.',
      paymentsList: ['Programmable paywalls', 'Instant settlement narrative', 'Verification API story']
    },
    faq: {
      eyebrow: 'FAQ',
      title: 'Demo notes',
      faqs: [
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
    },
    footer: {
      demo: 'Demo landing page',
      desc: 'Built for demo use. Replace placeholder copy, skills, and URLs when your backend is ready.',
      template: 'Template',
      features: 'Features',
      flow: 'Flow'
    }
  },
  zh: {
    nav: {
      marketplace: '技能市场',
      features: '功能特性',
      howItWorks: '工作原理',
      protocols: '协议',
      developers: '开发者'
    },
    hero: {
      eyebrow: 'AI技能经济的稳定币结账',
      title: '让你的Agent购买高级技能——不中断流程。',
      copy: 'StablePay为AI代理提供钱包支持的身份、干净的HTTP 402支付流程，以及开发者友好的付费墙模板。受MoltBay协议故事启发，但专注于Solana支付、技能货币化和ClawHub/OpenClaw演示场景。',
      viewCode: '查看代码模板',
      seeDemo: '查看演示流程',
      stats: {
        payment: '支付优先UX',
        solana: 'USDC/USDT重点',
        integration: '开发者集成目标'
      }
    },
    audience: {
      forUsers: '面向Agent用户',
      userTitle: '在对话中购买技能',
      userDesc: '创建钱包、绑定X、充值USDC，让你的代理自动购买低成本技能，或在更高价值任务上请求确认。',
      userList: ['钱包+DID入职', '自动购买阈值', '余额和交易历史'],
      forDevelopers: '面向技能开发者',
      devTitle: '使用简单模板货币化',
      devDesc: '复制支付代码片段，替换你的技能DID和价格，并在执行高级操作前可选地在后端验证购买。',
      devList: ['复制粘贴支付模板', '收入和销售占位符', '可选的验证API集成']
    },
    skills: {
      eyebrow: '演示市场',
      title: '你的代理可以发现和购买的占位符技能',
      desc: '这些卡片故意使用模拟数据。现在保留为占位符，或稍后用你的真实技能替换。',
      startingAt: '起价',
      items: [
        { icon: '✍️', name: '写作助手', handle: '@writer_agent', title: '长篇文章、简介和发布文案', tags: ['文案写作', '博客', '内容'], price: '$1.00', metric: '3.2k 安装' },
        { icon: '📊', name: '数据侦察员', handle: '@data_scout', title: '图表、仪表板和轻量级报告', tags: ['分析', '可视化', '报告'], price: '$2.00', metric: '2.4k 安装' },
        { icon: '🧠', name: '研究助手', handle: '@research_pilot', title: '快速研究摘要与来源链接', tags: ['研究', '摘要', '来源'], price: '$3.00', metric: '1.6k 安装' },
        { icon: '🎨', name: '设计草稿', handle: '@design_draft', title: '简单标志、横幅和产品视觉', tags: ['品牌', '视觉', '设计'], price: '$5.00', metric: '1.1k 安装' },
      ]
    },
    features: {
      eyebrow: '核心功能',
      title: '你的代理商务支付层',
      items: [
        { title: 'did:solana 身份', text: '为每位用户或开发者创建钱包支持的 DID，并将签名密钥保存在本地。' },
        { title: 'HTTP 402 支付', text: '通过标准的机器友好支付流程，为 AI 技能触发可编程付费墙。' },
        { title: 'X 验证 + 奖励', text: '绑定 X 账户、减少滥用，并在产品故事中演示注册奖励流程。' },
        { title: '快速集成', text: '开发者复制模板、替换 DID 和价格，几分钟内即可发布付费技能。' },
        { title: '开发者验证 API', text: '后端在执行高级操作前可验证购买，使付费墙更难被绕过。' },
        { title: 'Agent 原生 UX', text: '低价技能自动购买，高额操作请求确认，结果保持对话式呈现。' },
      ]
    },
    howItWorks: {
      eyebrow: '工作原理',
      title: 'StablePay的四个演示步骤'
    },
    steps: [
      {
        title: '创建钱包DID',
        text: 'StablePay为你的代理或开发者配置文件创建一个Solana钱包和did:solana身份。',
        code: 'did:solana:4fK9x2Hy...'
      },
      {
        title: '使用X验证',
        text: '发布验证推文，粘贴URL，并领取小额奖励以证明所有权并减少垃圾信息。',
        code: '验证并领取'
      },
      {
        title: '通过HTTP 402支付',
        text: '当高级技能返回Payment Required时，StablePay签署并完成购买流程。',
        code: '402 -> 签署支付 -> 200'
      },
      {
        title: '交付结果',
        text: '技能执行，开发者获得报酬，用户看到更新的余额和历史。',
        code: '余额: 47 USDC'
      }
    ],
    developers: {
      eyebrow: '开发者专区',
      title: '复制模板，替换占位符，发布付费技能',
      desc: '此区块设计为你可以现场演示的页面锚点。它符合你的产品方向：无仪表板、无登录墙，只是开发者可以粘贴到技能文档中的清晰模板。',
      apiTitle: '占位符API表面',
      copyTemplate: '复制模板',
      copied: '已复制!'
    },
    protocols: {
      identity: '身份',
      didSolana: 'did:solana',
      identityDesc: '为用户和开发者提供钱包支持的去中心化标识，具有本地签名和干净的所有权语义。',
      identityList: ['钱包创建', '签名验证', 'X绑定信任层'],
      payments: '支付',
      http402: 'HTTP 402 + Solana',
      paymentsDesc: '一个机器友好的付费墙，可以自动触发，以稳定币结算，并可选地由开发者后端验证。',
      paymentsList: ['可编程付费墙', '即时结算叙述', '验证API故事']
    },
    faq: {
      eyebrow: '常见问题',
      title: '演示说明',
      faqs: [
        {
          q: '这个页面是否生产就绪？',
          a: '这是一个精美的演示落地页。API调用和技能卡片是占位符，可以稍后连接到您的真实后端。'
        },
        {
          q: '为什么页面有类似MoltBay的结构？',
          a: '您要求参考moltbay.com，所以这个演示镜像其黑暗、协议优先的营销风格，同时为StablePay重写故事。'
        },
        {
          q: '我可以先改变什么？',
          a: '替换占位符技能，用真实端点交换演示URL，并用您的最终支付合约或API更新代码模板卡片。'
        }
      ]
    },
    footer: {
      demo: '演示落地页',
      desc: '为演示使用而构建。当你的后端准备就绪时，替换占位符副本、技能和URL。',
      template: '模板',
      features: '功能',
      flow: '流程'
    }
  }
}