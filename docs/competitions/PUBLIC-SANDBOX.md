# 公网支付宝沙箱与回调

部署日期：2026-10-28。服务器 `bubblevan.de5.net`（47.107.141.70），Linux amd64。

## 当前地址与验证

- PAYATHON沙箱页面：`https://bubblevan.de5.net/payathon`
- 健康检查：`https://bubblevan.de5.net/health`
- 支付宝通知：`https://bubblevan.de5.net/api/payments/alipay/notify`
- 支付后返回：`https://bubblevan.de5.net/payathon`

HTTPS使用已正常签发的公信证书，本机已通过正常证书校验访问。公开POST通知转发至payment-service的 `/v1/fiat/alipay/notify`。GET通知返回405，无签名的伪造成功通知返回400，公开访问 `/v1/fiat/orders` 返回404。

当前为 `alipay_sandbox`，`live_payments_enabled=false`。使用合成采购资料已验证用户确认、订单创建、沙箱收银台签名URL的通知与返回地址；未付款查询不交付报告。

**2026-10-28已完成一次用户手动沙箱付款及报告交付。** “活动预算核算”服务费CNY 1分，服务器16:36:14（Asia/Shanghai）收到通知并完成RSA2验签，支付状态 `PAID`、确认来源 `alipay-rsa2-notify`；16:36:21业务订单为 `DELIVERED`，报告已持久化。支付和业务订单的商品、金额、币种、报价、用户确认及交易号一致，记录有一次交付事件。报告预算¥500、合计¥536，提示超预算¥36。本次仅核算预算，不宣称已核验规格、审批或票据。

这不是正式收款或真实资金交易，不计入真实付费用户与收入。服务器检查未代替用户端文件下载验收。脱敏记录在 `.local-run/procurement-deploy/validation/paid-20261028.json`，操作步骤见[演示脚本](PAYATHON-DEMO-SCRIPT.md)。

校园AI工作台继续在本机 `http://127.0.0.1:8093/campus` 运行。本次公网部署为PAYATHON沙箱配置，未向服务器上传本机模型密钥，不代表校园AI公网版已部署。无需接入QMuse。

## 运行方式与资源

本机构建两个原生Go程序和React静态页面，服务器不执行Go/npm构建，不安装数据库或Docker。

- `stablepay-mvp-payment.service`：127.0.0.1:8092，沙箱支付订单与验签。
- `stablepay-mvp-runtime.service`：127.0.0.1:8093，报价、确认、报告交付和网页。
- `stablepay-mvp.slice`：总内存上限192MiB、MemoryHigh128MiB、CPU配额半核；两个程序各自上限96MiB，Go软内存目标48MiB。
- 部署后检查合计约13MiB，测得峰值约16MiB；这是当前低负载测量，不是压力测试结果。
- 状态：`/var/lib/stablepay-mvp/payment` 与 `/var/lib/stablepay-mvp/runtime`；服务重启不会删除订单。
- 配置与支付宝沙箱密钥：`/etc/stablepay-mvp`，限制本机权限，不在Git中。

已有Caddy的8080站点、`huiyi-edge-gateway` 和 `sni-gate` 保留。新增443站点通过单独Caddy片段接入，原8080首页部署后仍返回200。服务器存在出站443的SNI重定向，因此只对Caddy和新增服务用户增加带标记的直连例外，供证书签发和支付宝API使用；不清空或替换既有网关规则。

## 代码与发布

```powershell
# 从仓库根目录，在本机准备规范化PEM与最小沙箱配置；原始复制文件不改动。
node scripts/prepare-alipay-sandbox.mjs https://bubblevan.de5.net

# 本机交叉编译及打包，凭据不进入发布包。
powershell -NoProfile -ExecutionPolicy Bypass -File scripts/build-procurement-release.ps1
```

公开二进制包与SHA256位于忽略目录 `.local-run/procurement-deploy/`，最小凭据位于其中 `private/`。SCP仅上传选定的沙箱公私钥和两份服务配置，不上传SSH私钥、根 `.env`、模型密钥或本机订单。

服务器安装入口为 `deploy/procurement-mvp/install.sh`，先检查内存和磁盘，校验包摘要，安装独立systemd服务，等待本机健康检查成功，再校验并reload Caddy。原Caddy配置会备份，已有快照不会重置。业务进程使用专用低权限用户，私钥限制访问；安装和网络例外配置需root权限。

部署的发布目录为 `20261028-074605-linux-amd64`，应用来源为校园分支 `806ac2b` 中的共享代码，前端以PAYATHON mode构建。上线证据在本机忽略目录 `.local-run/procurement-deploy/validation/public.json`，测试Cookie和签名链接另存私有目录，不能公开提交。

## 下一步

用户在原浏览器打开已交付订单，点击“下载报告与交易回执 ↓”保存 `stablepay-receipt.json`，然后按[演示脚本](PAYATHON-DEMO-SCRIPT.md)录制。沙箱支付与服务端交付已通过一次实际交易验证；正式收款、AgentPay工具链及比赛投稿审核仍未完成。校园AI公网体验与真实试用继续作为独立待办。
