# StablePay · 校园活动采购守门员

这是 StablePay 参加支付宝「智能体涌现奖」的校园采购 MVP。粘贴活动需求与报价，真实模型抽取带原文引用的清单；用户确认后由服务端规则核对预算、规格及证据状态，并导出可追溯报告。

## 产品 Demo

```bash
npm install
npm run dev
```

- `/`、`/campus`：校园采购工作台，默认载入合成需求与报价，可演示¥536超出¥500及规格冲突。
- `/stablepay`：原 StablePay 支付产品介绍页。
- 在仓库根目录运行 `scripts/start-procurement-mvp.ps1 -Competition campus -UseLLM` 启动本机后端和静态站点；只运行前端不能使用模型或服务端预审。
- 点击“用AI抽取采购清单”调用配置模型，展开字段引用核对原文。人工编辑后明确确认，再运行预审；修改后必须重新确认，旧报告不能导出。
- 草稿保存在浏览器，原文和报告快照保存在本机服务；刷新和服务重启可恢复。服务端导出报告包含原文、字段引用及人工修改。

模型只整理资料，不设置审批或人工核验记录，也不执行付款。默认不加载校规；可选一所学校的有版本官网制度参考包，仅提供部分条款待办。文件真伪、供应商真实性和完整报销资格尚未自动核验。详见 [校园MVP与实际验收](../docs/competitions/AGENT-EMERGENCE-MVP.md)。

`npm test` 执行前端回归。完整测试、真实模型验收及未完成项见上述文档。`/stablepay` 为原支付介绍页，包含占位演示，不作为本次参赛能力或效果指标。

## PAYATHON 共用 MVP

新增 `/payathon` 入口，串联现有 Commerce Runtime 与 Payment Service 的 CNY 支付扩展。使用 `npm run dev:payathon` 或 `npm run build:payathon` 可将它设为默认首页。校园首页在普通构建中保持默认；`MVP_PROFILE=campus` 时可调用共用服务端核验。

完整的一键启动、支付环境配置与验收见 [共享 MVP](../docs/competitions/SHARED-MVP.md)。官方要求与未完成的真实支付条件见 [PAYATHON 准备记录](../docs/competitions/PAYATHON-2026.md)。

## 1. 本地运行

```bash
npm install
npm run dev
```

本地默认地址：

```bash
http://localhost:5173
```

## 2. 打包

```bash
npm run build
```

构建产物会输出到：

```bash
dist/
```

## 3. 页面包含什么

- Hero 区：讲 StablePay 的定位
- Marketplace 区：skills 占位卡片
- Features 区：did:solana / HTTP 402 / X verification / verify API 等
- How It Works 区：四步流程演示
- Developers 区：支付模板代码块
- Protocols 区：协议说明
- FAQ 区：演示说明

## 4. Nginx 部署思路

你可以有两种部署方式：

### 方式 A：直接让 Nginx 指向 Vite 的静态构建产物（推荐）

先在服务器上构建：

```bash
npm install
npm run build
```

然后把 `dist/` 部署到例如：

```bash
/var/www/wenfu.cc/dist
```

再使用 `deploy/nginx.conf` 里的 server 配置。

### 方式 B：本地构建后上传 dist

你也可以在你自己的电脑先执行：

```bash
npm install
npm run build
```

然后把生成的 `dist` 文件夹整体上传到服务器。

## 5. 服务器命令

假设你把整个项目放在服务器：

```bash
/root/stablepay-demo
```

执行：

```bash
cd /root/stablepay-demo
npm install
npm run build
mkdir -p /var/www/wenfu.cc
rm -rf /var/www/wenfu.cc/*
cp -r dist/* /var/www/wenfu.cc/
```

然后配置 Nginx：

```bash
cp deploy/nginx.conf /etc/nginx/sites-available/wenfu.cc
ln -sf /etc/nginx/sites-available/wenfu.cc /etc/nginx/sites-enabled/wenfu.cc
nginx -t
systemctl reload nginx
```

## 6. 如果你还没装 Nginx

Debian / Ubuntu：

```bash
apt update
apt install -y nginx
systemctl enable nginx
systemctl start nginx
```

## 7. 别忘了

- 阿里云安全组放行 80 端口
- 如果系统防火墙启用，也要放行 80
- 当前是 HTTP demo，不含 HTTPS 配置
