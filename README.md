# blockchain-adapter


## 文件结构树

blockchain-adapter/
├── idl/                              # [新增] IDL 定义层
│   └── blockchain.thrift             # 定义 RPC 接口 (供 Payment/Query 服务调用)
│
├── kitex_gen/                        # [新增] Kitex 自动生成代码 (通过 thrift 编译自动生成)
│   └── blockchain/                   # 包含 RPC Client 和 Server 的接口桩代码 (勿动)
│
├── conf/                             # [新增] 配置层
│   ├── dev.yaml                      # 开发环境配置 (Solana RPC 节点、MySQL DSN、热钱包路径等)
│   └── prod.yaml                     # 生产环境配置
│
├── cmd/                              # 启动入口层
│   └── server/
│       └── main.go                   # 微服务启动入口 (初始化 Kitex Server, 读取配置, 连接 DB)
│
├── internal/                         # 私有业务逻辑层 (核心)
│   ├── handler/                      # [新增] RPC 接口实现层 (Controller)
│   │   └── blockchain_handler.go     # 实现 blockchain.thrift 中定义的方法 (如 Transfer, GetBalance)
│   │
│   ├── service/                      # [新增] 业务逻辑编排层 (Service)
│   │   ├── transfer_service.go       # 组合调用 solana 层和 dal 层，完成带 Gas 补贴的转账
│   │   └── balance_service.go        # 处理余额查询等逻辑
│   │
│   ├── solana/                       # [迁移自 POC] Solana 交互底层
│   │   ├── client.go                 # RPC 客户端封装
│   │   ├── transaction.go            # 交易构建器 (含 FeePayer)
│   │   └── keypair_manager.go        # 平台热钱包密钥加载与管理
│   │
│   ├── crypto/                       # [迁移自 POC] 密码学层
│   │   └── keypair.go                # Ed25519 生成、签名验证、DID 构造
│   │
│   └── dal/                          # [新增] 数据访问层 (Data Access Layer)
│       └── db/                       # MySQL 数据库交互 (GORM)
│           ├── init.go               # MySQL 连接初始化
│           └── gas_subsidy.go        # 将 POC 中的 JSON 存储改为写入 MySQL
│
├── pkg/                              # 公共包 (可供其他微服务复制或引用)
│   ├── errno/                        # 全局错误码定义 (参考方案 9.4)
│   └── utils/                        # 基础工具函数
│
├── script/                           # 脚本
│   └── build.sh                      # 编译与打包脚本
│
├── go.mod                            # 依赖管理
└── README.md                         # 微服务说明文档
*POC内容见demo1*

## 结构说明与推进指南 (从 POC 到 Kitex)：
idl/blockchain.thrift 作为总纲：
在正式写 Go 代码前，需要对齐定义这个 Thrift 文件。明确 Payment Service 需要blockchain-adapter提供哪些接口（比如 TransferSOL, GetBalance），然后使用 kitex 命令行工具生成 kitex_gen 目录。
internal/handler 替换了 POC 中的 cmd/solana_poc：
POC 是通过写 main.go 命令行脚本来触发转账；在微服务中，是由 handler/blockchain_handler.go 接收到 RPC 请求后触发转账。
internal/service 充当胶水层：
POC 里的 transfer.go 包含了较多业务逻辑，现在应该升级为 service/transfer_service.go。它负责：验证参数 -> 调用 solana/transaction.go 构建交易 -> 调用 crypto 双重签名 -> 提交上链 -> 调用 dal/db 将补贴记录存入 MySQL。
推进持久化 (data-access-layer/db)：
POC 阶段的 config/gas_subsidies.json 需要升级。现在应该引入 gorm，在 data-access-layer/db/gas_subsidy.go 中，将交易状态和 Gas 费补贴详情写入真实的 MySQL 数据库。