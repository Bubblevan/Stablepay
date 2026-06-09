export const translations = {
  en: {
    nav: {
      forAIUsers: "I'm AI User",
      forAIDevelopers: "I'm AI Developer",
      walletGuide: 'Wallet Guide'
    },
    walletGuide: {
      eyebrow: 'Before You Start',
      title: 'Wallet & Crypto Basics',
      subtitle: 'Essential concepts for using StablePay effectively',
      usdc: {
        title: 'What is USDC?',
        desc: 'USDC is a digital dollar (stablecoin) that maintains a 1:1 value with the US Dollar. Unlike volatile cryptocurrencies like Bitcoin, 1 USDC always equals approximately $1 USD.',
        highlight: 'StablePay uses USDC for all payments because it\'s stable, fast, and widely accepted.'
      },
      wallet: {
        title: 'Your Solana Wallet',
        desc: 'A wallet is your digital identity on the blockchain. It has a public address (like a bank account number) and private keys (like your password).',
        points: [
          'Public Address: Share this to receive payments',
          'Private Key / Seed Phrase: NEVER share this with anyone',
          'Stored locally: Your keys stay on your device, not our servers'
        ],
        example: 'Solana Address: 7xKXtg2CW87d97TXJSDpbD5jBkheTqA83TZRuJosgAsU'
      },
      did: {
        title: 'Your DID Identity',
        desc: 'DID (Decentralized Identifier) is your unique identity in StablePay. It\'s derived from your wallet and looks like this:',
        example: 'did:solana:7xKXtg2CW87d97TXJSDpbD5jBkheTqA83TZRuJosgAsU'
      },
      signature: {
        title: 'Transaction Signing',
        desc: 'When you make a payment, you "sign" the transaction with your private key. This proves you authorized the payment.',
        warning: 'Every payment requires your confirmation. Review the amount and recipient before signing.'
      }
    },
    hero: {
      eyebrow: 'Programmable payments for autonomous agents',
      title: 'Let your AI Agent pay for and access digital services',
      copy: 'StablePay gives AI agents a wallet-backed identity, an HTTP 402/x402-ready payment flow, reusable access credentials, and developer-friendly paywall templates. It helps agents purchase paid APIs, MCP tools, datasets, content, and services with stablecoins while keeping authorization and settlement verifiable.',
      viewCode: 'View Integration Template',
      seeDemo: 'See Payment Flow',
      stats: {
        payment: 'agent-native checkout',
        solana: 'USDC settlement',
        integration: 'x402-ready integration'
      }
    },
    audience: {
      forUsers: 'For AI Users',
      userTitle: 'Enable your AI with a payment wallet',
      userDesc: 'Create a wallet-backed identity for your AI agent, configure spending limits, top up stablecoins, and let your AI autonomously pay for services and resources it needs.',
      userList: ['AI wallet + DID setup', 'Spending limits and confirmations', 'Balance tracking and payment history'],
      forDevelopers: 'For AI Developers',
      devTitle: 'Monetize your AI services and APIs',
      devDesc: 'Add programmable payments to your AI service, API, MCP server, or agent tool. StablePay handles the payment flow, access verification, and credential management for you.',
      devList: ['Quick-start paywall templates', 'HTTP 402 / x402 payment protocol', 'Backend verification API']
    },
    skills: {
      eyebrow: 'Resource Marketplace',
      title: 'Paid resources your agent could discover and use',
      desc: 'APIs, MCP tools, datasets, content, and services that agents can discover, pay for, and access programmatically.',
      startingAt: 'Starting at',
      items: [
        { icon: '🔌', name: 'API Data Feed', handle: '@market_api', title: 'Real-time structured data for agent workflows', tags: ['api', 'data', 'automation'], price: '$1.00', metric: '3.2k calls' },
        { icon: '🧰', name: 'MCP Tool Server', handle: '@tool_server', title: 'Paid tools exposed through an agent-readable interface', tags: ['mcp', 'tools', 'agent'], price: '$2.00', metric: '2.4k sessions' },
        { icon: '📚', name: 'Research Dataset', handle: '@data_vault', title: 'Curated datasets and source-backed research packages', tags: ['dataset', 'research', 'sources'], price: '$3.00', metric: '1.6k purchases' },
        { icon: '🤖', name: 'Agent Service', handle: '@service_agent', title: 'Specialized agent capabilities available on demand', tags: ['service', 'workflow', 'automation'], price: '$5.00', metric: '1.1k uses' },
      ]
    },
    features: {
      eyebrow: 'Core features',
      title: 'A payment and access layer for agent commerce',
      items: [
        { title: 'Wallet-backed agent identity', text: 'Create a DID for every agent, user, or provider, with signing keys kept local and ownership easy to verify.' },
        { title: 'HTTP 402 / x402 payments', text: 'Expose machine-readable payment requirements so agents can understand pricing, pay, and continue the request flow.' },
        { title: 'Reusable access credentials', text: 'After payment, issue access credentials so repeated calls can be verified off-chain without paying every time.' },
        { title: 'Spending controls', text: 'Set auto-pay thresholds, confirmation rules, and budget limits for safer autonomous agent payments.' },
        { title: 'Developer verification API', text: 'Backends can verify payment status or access credentials before serving paid APIs, tools, data, or services.' },
        { title: 'Agent-native UX', text: 'Agents can pay, retry, access, and report results inside the same workflow instead of sending users through checkout pages.' },
      ]
    },
    howItWorks: {
      eyebrow: 'How it works',
      title: 'StablePay in four steps'
    },
    steps: [
      {
        title: 'Create an agent identity',
        text: 'StablePay creates a wallet-backed DID for an agent, user, or resource provider.',
        code: 'did:solana:4fK9x2Hy...'
      },
      {
        title: 'Configure payment rules',
        text: 'Set stablecoin balance, spending limits, auto-pay thresholds, and confirmation rules for agent purchases.',
        code: 'limit: 10 USDC / session'
      },
      {
        title: 'Pay via HTTP 402',
        text: 'When a paid API, tool, dataset, or service returns Payment Required, StablePay signs and completes the payment flow.',
        code: '402 -> signed pay -> 200'
      },
      {
        title: 'Reuse access',
        text: 'After payment, StablePay can verify reusable access credentials so later requests avoid repeated on-chain payment.',
        code: 'access: reusable'
      }
    ],
    developers: {
      eyebrow: 'Developer zone',
      title: 'Add payment to your agent-facing resource',
      desc: 'Use StablePay to protect APIs, MCP tools, datasets, content pages, or agent services with an HTTP 402 payment flow and backend verification.',
      apiTitle: 'StablePay API surface',
      copyTemplate: 'Copy Template',
      copied: 'Copied!',
      viewDocs: 'View full developer documentation'
    },
    protocols: {
      identity: 'Identity',
      didSolana: 'did:solana',
      identityDesc: 'Wallet-backed decentralized identifiers for agents, users, and providers, with local signing and clean ownership semantics.',
      identityList: ['Wallet creation', 'Signature verification', 'Agent ownership semantics'],
      payments: 'Payments',
      http402: 'HTTP 402 / x402 + Solana',
      paymentsDesc: 'A machine-friendly payment layer for paid APIs, tools, data, content, and services, settled in stablecoins and verifiable by backend systems.',
      paymentsList: ['Programmable payment requirements', 'Stablecoin settlement', 'Access verification API']
    },
    quickStart: {
      eyebrow: 'Get Started',
      title: 'Quick Start Guide',
      install: {
        title: '1. Install Plugin & Configure',
        desc: 'Install the StablePay plugin in OpenClaw by entering the command below:'
      },
      wallet: {
        title: '2. Chat with OpenClaw to Initialize',
        desc: 'After installing the plugin, simply chat with OpenClaw to start initialization. OpenClaw will automatically guide you through wallet creation and configuration:',
        steps: [
          "Help me initialize StablePay plugin",
          "(OpenClaw will automatically detect status and ask questions, e.g., No wallet detected. Would you like to create a new wallet?)",
          "(After confirming, OpenClaw will create a wallet and ask if you want to configure payment limits)",
          "(Once configured, you can start using it)"
        ]
      },
      example: {
        title: 'Conversation Example',
        desc: 'Here is a real initialization conversation example:',
        user1: 'Help me initialize StablePay plugin',
        agent1: 'No wallet detected. Would you like to create a new wallet?\n\nCreate command:\nstablepay_create_local_wallet --user_id your_name\n\nConfirm creation?',
        user2: 'Create one',
        agent2: 'Wallet created successfully!\nNew wallet info:\nAddress: 6Hhpdd8NWDN5D3rt8cGYoR24Fwfcrb2QC4s6Fz6qTqJB\nDID: did:solana:6Hhpdd8NWDN5D3rt8cGYoR24Fwfcrb2QC4s6Fz6qTqJB\nWallet name: stablepay-your_name\n\nWould you like to configure payment limits next?',
        user3: 'Configure it',
        agent3: 'Configuration complete!\nPayment limits:\nSingle purchase limit: 10 USDC\nAuto-pay threshold: 1 USDC (below this auto-confirms, above requires manual approval)\nCurrency: USDC\n\nConfiguration complete. You can now:\n\n1. Query balance\nstablepay_query_balance --did did:solana:...\n\n2. Execute paid skill\nstablepay_execute_paid_skill_demo\n\n3. Pay via gateway\nstablepay_pay_via_gateway --skill_name xxx --price 1.00\n\n4. Query sales records\nstablepay_query_sales --skill_did xxx'
      },
      merchant: {
        title: '5. Try the Merchant Demo',
        desc: 'Clone the merchant example to test end-to-end payment flow:',
        step1: 'Copy the skills',
        step2: 'Run the merchant backend',
        step3: 'Return to OpenClaw and continue the conversation',
        finalStep: 'Check the ShowMeTheMoney skill in your skills list and follow the flow to try the merchant paid demo.',
        note: 'Check showmethemoney-pro and merchant-backend. The merchant backend verifies StablePay purchases before executing premium actions.'
      }
    },
    faq: {
      eyebrow: 'Quick Start',
      title: 'Start using StablePay with OpenClaw',
      faqs: [
        {
          q: 'How do I install the StablePay OpenClaw plugin?',
          a: 'Install the plugin in OpenClaw with: openclaw plugins install clawhub:stablepay-agentpay-dev@0.3.11 --force --dangerously-force-unsafe-install. After installation, create ~/.openclaw/.env and configure the local encryption key and platform fee payer settings required by the plugin.',
        },
        {
          q: 'What does the local master key do?',
          a: 'STABLEPAY_PLUGIN_MASTER_KEY is used to encrypt and decrypt the local StablePay state file. The encrypted file stores wallet metadata, DID information, payment policy, spending limits, and runtime configuration. Keep this key local and never publish it in frontend code, documentation screenshots, or public repositories.',
        },
        {
          q: 'How do I create or bind an agent wallet?',
          a: 'In OpenClaw, ask the agent to inspect the installed StablePay plugin tools and runtime status, then create or bind an OWS wallet. The plugin can create a wallet through the OWS SDK runtime and use the Solana address as the wallet-backed identity for StablePay payments.',
        },
        {
          q: 'How do I configure spending limits?',
          a: 'After the wallet is ready, query the USDC balance through Solana RPC and configure a payment policy. For example, you can set an auto-pay threshold of 0.6 USDC and a maximum single purchase limit of 10 USDC so small agent purchases can complete automatically while larger payments still require confirmation.',
        },
        {
          q: 'How does DID registration work?',
          a: 'StablePay registers the buyer identity by submitting the wallet public key, wallet address, wallet name, and signing runtime to the DID service. After registration, the agent receives a did:solana identifier that can be used in payment, verification, and access-control flows.',
        },
        {
          q: 'How can I try a paid resource end to end?',
          a: 'Clone the merchant example repository, start the merchant backend, and run the protected ShowMeTheMoney Pro flow. The merchant backend verifies the StablePay purchase before executing the premium action, so users can see the full payment-to-access path.',
        },
        {
          q: 'What happens when an agent reaches a paid resource?',
          a: 'The resource returns HTTP 402 Payment Required. StablePay builds the payment request, signs it through the configured wallet runtime, submits the payment, verifies the result, and then allows the protected API, tool, or service to execute.',
        },
        {
          q: 'Is StablePay only for OpenClaw skills?',
          a: 'No. OpenClaw is the first integration scenario. StablePay is designed as a general agent payment layer for paid APIs, MCP tools, datasets, content, services, and other agent-accessible resources.',
        },
        {
          q: 'Does it work on Windows, Linux, and Mac?',
          a: 'Yes, all platforms are supported. Windows users will automatically fall back to CLI mode if SDK loading fails. Linux and Mac usually work directly with SDK mode.',
        },
        {
          q: 'What is the difference between OWS CLI and OWS SDK?',
          a: 'OWS SDK is a JavaScript library that the plugin can import and call directly; OWS CLI is a command-line tool that requires running ows commands in the terminal. The plugin prefers SDK, and if SDK is unavailable (e.g., on Windows), it automatically falls back to CLI mode. If using CLI mode, you need to install globally: npm install -g @open-wallet-standard/core.',
        },
      ]
    },
    footer: {
      demo: 'StablePay Agent Payment',
      desc: 'Programmable payments and access verification for agent-facing APIs, tools, data, content, and services.',
      template: 'Template',
      features: 'Features',
      flow: 'Flow'
    }
  },
  zh: {
    nav: {
      forAIUsers: '我是AI用户',
      forAIDevelopers: '我是AI开发者',
      walletGuide: '钱包指南'
    },
    walletGuide: {
      eyebrow: '开始之前',
      title: '钱包与加密货币基础',
      subtitle: '使用 StablePay 前需要了解的基本概念',
      usdc: {
        title: '什么是 USDC？',
        desc: 'USDC 是一种数字美元（稳定币），与美元保持 1:1 的价值锚定。与比特币等波动较大的加密货币不同，1 USDC 始终约等于 1 美元。',
        highlight: 'StablePay 使用 USDC 进行所有支付，因为它稳定、快速且被广泛接受。'
      },
      wallet: {
        title: '你的 Solana 钱包',
        desc: '钱包是你在区块链上的数字身份。它有一个公开地址（类似银行卡号）和私钥（类似你的密码）。',
        points: [
          '公开地址：分享此地址用于接收付款',
          '私钥 / 助记词：切勿与任何人分享',
          '本地存储：你的密钥保存在设备上，而非我们的服务器'
        ],
        example: 'Solana 地址：7xKXtg2CW87d97TXJSDpbD5jBkheTqA83TZRuJosgAsU'
      },
      did: {
        title: '你的 DID 身份',
        desc: 'DID（去中心化标识符）是你在 StablePay 中的唯一身份。它从你的钱包派生，格式如下：',
        example: 'did:solana:7xKXtg2CW87d97TXJSDpbD5jBkheTqA83TZRuJosgAsU'
      },
      signature: {
        title: '交易签名',
        desc: '当你进行支付时，你会用私钥对交易进行"签名"。这证明你授权了该笔支付。',
        warning: '每笔支付都需要你的确认。签名前请仔细核对金额和收款方。'
      }
    },
    hero: {
      eyebrow: '面向自主 Agent 的支付',
      title: '让你的 AI Agent 自主购买并支付',
      copy: 'StablePay 为 AI Agent 提供钱包支持的身份、HTTP 402/x402-ready 支付流程、可复用访问凭证，以及开发者友好的付费模板。通过帮助 Agent 使用稳定币购买付费 API、MCP 工具、数据集、内容和服务，同时保证授权与结算可验证。',
      viewCode: '查看集成模板',
      seeDemo: '查看支付流程',
      stats: {
        payment: 'Agent 原生结账',
        solana: 'USDC 结算',
        integration: 'x402-ready 集成'
      }
    },
    audience: {
      forUsers: '面向AI用户',
      userTitle: '为AI配备支付钱包',
      userDesc: '为你的AI Agent创建钱包身份，配置消费限额，充值稳定币，让你的AI自主支付所需服务和资源。',
      userList: ['AI钱包 + DID设置', '消费限额和确认规则', '余额追踪和支付历史'],
      forDevelopers: '面向AI开发者',
      devTitle: '将你的AI服务变现',
      devDesc: '为你的AI服务、API、MCP服务器或Agent工具添加可编程支付。StablePay处理支付流程、访问验证和凭证管理。',
      devList: ['快速启动付费墙模板', 'HTTP 402 / x402支付协议', '后端验证API']
    },
    skills: {
      eyebrow: '资源市场',
      title: 'Agent 可以发现并使用的付费资源',
      desc: 'API、MCP 工具、数据集、内容和服务，Agent 可以通过程序化方式进行发现、支付和访问。',
      startingAt: '起价',
      items: [
        { icon: '🔌', name: 'API 数据源', handle: '@market_api', title: '面向 Agent 工作流的实时结构化数据', tags: ['API', '数据', '自动化'], price: '$1.00', metric: '3.2k 次调用' },
        { icon: '🧰', name: 'MCP 工具服务', handle: '@tool_server', title: '通过 Agent 可读接口暴露的付费工具', tags: ['MCP', '工具', 'Agent'], price: '$2.00', metric: '2.4k 次会话' },
        { icon: '📚', name: '研究数据集', handle: '@data_vault', title: '经过整理的数据集与带来源的研究包', tags: ['数据集', '研究', '来源'], price: '$3.00', metric: '1.6k 次购买' },
        { icon: '🤖', name: 'Agent 服务', handle: '@service_agent', title: '可按需调用的专用 Agent 能力', tags: ['服务', '工作流', '自动化'], price: '$5.00', metric: '1.1k 次使用' },
      ]
    },
    features: {
      eyebrow: '核心功能',
      title: '面向 Agent Commerce 的支付与访问层',
      items: [
        { title: '钱包支持的 Agent 身份', text: '为每个 Agent、用户或资源提供方创建 DID，将签名密钥保存在本地，并让所有权易于验证。' },
        { title: 'HTTP 402 / x402 支付', text: '暴露机器可读的支付要求，让 Agent 能理解价格、完成付款，并继续原有请求流程。' },
        { title: '可复用访问凭证', text: '支付成功后签发访问凭证，后续重复调用可以链下验证，无需每次都重新支付。' },
        { title: '消费控制', text: '设置自动付款阈值、确认规则和预算上限，让自主 Agent 支付更安全。' },
        { title: '开发者验证 API', text: '后端可以在提供付费 API、工具、数据或服务前，验证支付状态或访问凭证。' },
        { title: 'Agent 原生体验', text: 'Agent 可以在同一任务流中完成支付、重试、访问和结果汇报，而不是跳转到传统结账页面。' },
      ]
    },
    howItWorks: {
      eyebrow: '工作原理',
      title: 'StablePay 的四步骤'
    },
    steps: [
      {
        title: '创建 Agent 身份',
        text: 'StablePay 为 Agent、用户或资源提供方创建一个钱包支持的 DID。',
        code: 'did:solana:4fK9x2Hy...'
      },
      {
        title: '配置支付规则',
        text: '设置稳定币余额、消费限额、自动付款阈值和确认规则。',
        code: 'limit: 10 USDC / session'
      },
      {
        title: '通过 HTTP 402 支付',
        text: '当付费 API、工具、数据集或服务返回 Payment Required 时，StablePay 签名并完成支付流程。',
        code: '402 -> 签名支付 -> 200'
      },
      {
        title: '复用访问权',
        text: '支付成功后，StablePay 可以验证可复用访问凭证，后续请求不必重复进行链上支付。',
        code: 'access: reusable'
      }
    ],
    developers: {
      eyebrow: '开发者专区',
      title: '为 Agent 可访问资源添加支付能力',
      desc: '使用 StablePay 为 API、MCP 工具、数据集、内容页面或 Agent 服务接入 HTTP 402 支付流程和后端验证能力。',
      apiTitle: 'StablePay API 能力',
      copyTemplate: '复制模板',
      copied: '已复制!',
      viewDocs: '查看完整开发者文档'
    },
    protocols: {
      identity: '身份',
      didSolana: 'did:solana',
      identityDesc: '为 Agent、用户和资源提供方提供钱包支持的去中心化标识，具备本地签名和清晰的所有权语义。',
      identityList: ['钱包创建', '签名验证', 'Agent 所有权语义'],
      payments: '支付',
      http402: 'HTTP 402 / x402 + Solana',
      paymentsDesc: '面向付费 API、工具、数据、内容和服务的机器友好支付层，可用稳定币结算，并由后端系统进行验证。',
      paymentsList: ['可编程支付要求', '稳定币结算', '访问验证 API']
    },
    quickStart: {
      eyebrow: '快速开始',
      title: '快速上手指南',
      install: {
        title: '1. 安装插件与配置',
        desc: '在 OpenClaw 中安装 StablePay 插件：（请在终端中输入）'
      },
      wallet: {
        title: '2. 与 OpenClaw 对话初始化',
        desc: '安装插件后，直接与 OpenClaw 对话即可开始初始化。OpenClaw 会自动引导你完成钱包创建和配置：',
        steps: [
          '帮我初始化 StablePay 插件',
          '（OpenClaw 会自动检测状态并提问，如：检测到没有钱包，需要创建一个新钱包吗？）',
          '（确认创建后，OpenClaw 会创建钱包并询问是否配置支付限额）',
          '（配置完成后即可开始使用）'
        ]
      },
      example: {
        title: '对话示例',
        desc: '以下是一个真实的初始化对话示例：',
        user1: '帮我初始化 StablePay 插件',
        agent1: '检测到没有钱包。需要创建一个新钱包吗？\n\n创建命令：\nstablepay_create_local_wallet --user_id your_name\n\n确认创建吗？',
        user2: '创建一个',
        agent2: '钱包创建成功！\n新钱包信息：\n地址：123456789abcdefghijklmnopqrsduvwxyz\nDID：did:solana:123456789abcdefghijklmnopqrsduvwxyz\n钱包名：stablepay-your_name\n\n接下来需要配置支付限额吗？',
        user3: '配置一下',
        agent3: '配置完成！\n支付限额设置：\n单次购买上限：10 USDC\n自动支付阈值：1 USDC（低于此金额自动确认，高于需手动确认）\n货币：USDC\n\n配置完成了。现在你可以：\n\n1. 查询余额\nstablepay_query_balance --did did:solana:...\n\n2. 执行付费技能\nstablepay_execute_paid_skill_demo\n\n3. 通过网关支付\nstablepay_pay_via_gateway --skill_name xxx --price 1.00\n\n4. 查询销售记录\nstablepay_query_sales --skill_did xxx'
      },
      merchant: {
        title: '3. 体验商家示例（可选）',
        desc: '克隆商家示例仓库，测试端到端支付流程：',
        step1: '拷贝skills',
        step2: '运行商家客户端',
        step3: '回到OpenClaw并对话',
        finalStep: '请查看你技能列表里的 showmethemoney 技能，并按照流程体验一次商家的付费示例服务。',
        note: '查看 showmethemoney-pro 和 merchant-backend。商家后端会在执行高级动作前验证 StablePay 支付。'
      }
    },
    faq: {
      eyebrow: '快速上手',
      title: '在 OpenClaw 中开始使用 StablePay',
      faqs: [
        {
          q: '如何安装 StablePay OpenClaw 插件？',
          a: '在 OpenClaw 中执行安装命令：openclaw plugins install clawhub:stablepay-agentpay-dev@0.3.11 --force --dangerously-force-unsafe-install。安装完成后，创建 ~/.openclaw/.env 文件，并配置插件需要读取的本地加密密钥和平台 fee payer 设置。'
        },
        {
          q: '本地 master key 是做什么的？',
          a: 'STABLEPAY_PLUGIN_MASTER_KEY 用于加密和解密本地 StablePay 状态文件。该加密文件会保存钱包信息、DID 信息、支付策略、消费限额和运行时配置。这个密钥只能保存在本地，不应该出现在前端代码、公开文档截图或公开仓库中。'
        },
        {
          q: '如何创建或绑定 Agent 钱包？',
          a: '在 OpenClaw 对话中，可以先让 Agent 检查当前安装的 StablePay 插件工具和运行时状态，然后创建或绑定一个 OWS 钱包。插件可以通过 OWS SDK runtime 创建钱包，并使用 Solana 地址作为 StablePay 支付中的钱包身份。'
        },
        {
          q: '如何配置支付限额？',
          a: '钱包准备好之后，可以通过 Solana RPC 查询 USDC 余额，并配置支付策略。例如，将自动购买阈值设置为 0.6 USDC，将单次购买上限设置为 10 USDC。这样低额 Agent 支付可以自动完成，高额支付仍然需要用户确认。'
        },
        {
          q: 'DID 注册是如何完成的？',
          a: 'StablePay 会把钱包公钥、钱包地址、钱包名称和签名运行时提交给 DID 服务，注册买家身份。注册成功后，Agent 会获得一个 did:solana 标识，用于后续支付、验证和访问控制流程。'
        },
        {
          q: '如何完整体验一次付费资源调用？',
          a: '可以克隆商家示例仓库，启动 merchant backend，然后运行 ShowMeTheMoney Pro 保护动作。商家后端会在执行高级动作前验证 StablePay 支付结果，从而展示从支付到访问授权的完整链路。'
        },
        {
          q: 'Agent 访问付费资源时会发生什么？',
          a: '付费资源会返回 HTTP 402 Payment Required。StablePay 随后构造支付请求，通过配置的钱包运行时完成签名，提交支付，验证结果，并在支付成功后放行受保护的 API、工具或服务。'
        },
        {
          q: 'StablePay 只能用于 OpenClaw 技能吗？',
          a: '不是。OpenClaw 是第一个集成场景。StablePay 的定位是更一般化的 Agent Payment 层，可用于付费 API、MCP 工具、数据集、内容、服务和其他 Agent 可访问资源。'
        },
        {
          q: 'Windows、Linux、Mac 都能用吗？',
          a: '都可以。Windows 用户如果 SDK 加载失败，会自动回退到 CLI 模式。Linux 和 Mac 通常 SDK 模式可以直接工作。'
        },
        {
          q: 'OWS CLI 和 OWS SDK 有什么区别？',
          a: 'OWS SDK 是 JavaScript 库，插件可以直接 import 调用；OWS CLI 是命令行工具，需要在终端执行 ows 命令。插件优先使用 SDK，如果 SDK 不可用（如 Windows 环境），会自动回退到 CLI 模式。如果使用 CLI 模式，需要全局安装：npm install -g @open-wallet-standard/core。'
        }
      ]
    },
    footer: {
      demo: 'StablePay Agent Payment',
      desc: '面向 Agent 可访问 API、工具、数据、内容和服务的可编程支付与访问验证层。',
      template: '模板',
      features: '功能',
      flow: '流程'
    }
  }
}