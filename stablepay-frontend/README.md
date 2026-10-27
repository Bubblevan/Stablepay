# StablePay · 校园活动采购守门员

这是 StablePay 参加支付宝「智能体涌现奖」的产品原型，主线是帮助学生组织者在付款前检查活动预算、采购规格、负责人审批状态和票据准备情况。

## 产品 Demo

```bash
npm install
npm run dev
```

- `/`：校园活动采购预审工作台，默认载入一份超预算、规格不符且票据条件待确认的示例清单。
- `/stablepay`：原 StablePay 支付产品介绍页。
- 修改预算、报价、规格或审批状态后，点击“开始采购预审”重新运行浏览器内的确定性检查；预算批复依据和逐项报价来源会写入导出清单。
- 检查结果可下载为 Markdown 预审清单；当前数据仅保存在页面状态中，刷新后恢复示例。

当前检查器使用用户填写的事实做规则核验，不调用 LLM，也没有绑定任何学校的正式制度。预审输出不构成学校审批或报销保证。

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
