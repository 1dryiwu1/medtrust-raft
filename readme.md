# MedTrust｜患者授权型医疗数据联盟链系统

重庆工程学院 · 2026 年比赛演示版。以下账户、病历和附件均为虚构测试数据，项目为本地技术原型，不是接入真实医院的生产系统。

## 评委快速体验

Windows 10/11 64 位用户可直接下载 [含测试数据的安装包](./downloads/MedTrust-Competition-Setup-20260928.exe)（约 11.5 MiB）。安装后从开始菜单选择“MedTrust 比赛演示版 → 启动并打开 MedTrust”，浏览器会打开 <http://127.0.0.1:8001/>；结束后从开始菜单选择“停止 MedTrust”。安装包已包含三个节点程序、前端页面、测试数据库和附件，无需安装 Go 或 Node.js。

| 角色 | 测试用户名 | 测试密码 | 可体验内容 |
| --- | --- | --- | --- |
| 管理员 | `admin` | `admin123` | 医生管理、联盟链后台 |
| 患者 | `securep164624` | `Medtrust123` | 病历查看、授权与上链确认 |
| 医生 | `secured164624` | `Medtrust123` | 实名认证医生的病历与诊疗流程 |

以上密码仅用于离线比赛演示。请勿在公开网络运行本演示数据库、复用这些密码，或录入真实患者信息。数据库存储的是密码哈希；新账户使用独立随机盐的 Argon2id，旧测试账户登录后可迁移。

如需查看或修改系统实现，请继续阅读下方源码运行说明。安装包与源码的用途不同：安装包自带测试数据，源码仓库不直接提交运行中的数据库文件。

MedTrust-Raft 是一个基于 Raft 共识的医疗数据联盟链演示项目。  
后端使用 Go 实现 Raft 节点、HTTP API 和 BoltDB 持久化，前端使用 Vue 3 + Vite 展示节点状态、区块链账本和数据上链流程。

## 项目目标

- 演示三节点联盟链集群的启动、选举、复制与提交
- 演示医疗数据上链后的区块持久化与账本展示
- 为比赛评审提供带测试数据的可运行样例

## 技术栈

- 后端：Go、Gin、BoltDB
- 共识：自研 Raft
- 前端：Vue 3、Vite
- 节点通信：`net/rpc over HTTP`

## 项目结构

```text
.
├── cmd/node/main.go
├── configs/
│   ├── node1.yaml
│   ├── node2.yaml
│   └── node3.yaml
├── internal/
│   ├── api/
│   ├── blockchain/
│   ├── raft/
│   ├── store/
│   └── transport/
├── scripts/
│   ├── start_cluster.sh
│   ├── start_cluster.ps1
│   └── start_cluster.cmd
└── web/
```

## 环境要求

- Go 1.23 或更高
- Node.js 18 或更高
- npm
- Windows 用户推荐 PowerShell
- 可选：Git Bash，用于执行 `start_cluster.sh`

## 快速启动

本节面向从源码运行的开发者。源码仓库不包含预编译程序、前端构建产物或运行中的数据库；希望直接体验预置账户与病历，请使用上方比赛安装包。源码运行时，先构建前端和后端，再启动三节点集群：

```powershell
cd web
npm ci
npm run build
cd ..
go build -o medtrust-node.exe ./cmd/node
powershell -ExecutionPolicy Bypass -File .\scripts\start_release.ps1 start
```

全新空数据库需先设置 `MEDTRUST_ADMIN_PASSWORD` 环境变量，供首次创建管理员账户使用。启动后访问 <http://127.0.0.1:8001/>；停止命令为 `powershell -ExecutionPolicy Bypass -File .\scripts\start_release.ps1 stop`。

详细步骤见 [门户网站运行说明](./docs/门户网站运行说明.md)。以下命令用于开发和调试三节点集群。

### Windows 推荐启动方式

在项目根目录执行：

```powershell
powershell -ExecutionPolicy Bypass -File .\scripts\start_cluster.ps1 start
```

或使用更短的命令：

```cmd
scripts\start_cluster.cmd start
```

常用命令：

```powershell
powershell -ExecutionPolicy Bypass -File .\scripts\start_cluster.ps1 status
powershell -ExecutionPolicy Bypass -File .\scripts\start_cluster.ps1 stop
powershell -ExecutionPolicy Bypass -File .\scripts\start_cluster.ps1 reset
```

对应的 `.cmd` 包装命令也可直接使用：

```cmd
scripts\start_cluster.cmd status
scripts\start_cluster.cmd stop
scripts\start_cluster.cmd reset
```

说明：

- `start`：自动编译并后台启动 3 个节点
- `status`：检查 3 个节点 API 是否在线
- `stop`：停止所有 `medtrust-node.exe` 进程
- `reset`：停止节点并清空 `data/node-*/chain.db`

注意：

- `reset` 不会自动重启集群
- 想恢复为全新集群时，请执行 `reset` 后再执行 `start`

### Bash 启动方式

如果你使用 Git Bash：

```bash
bash scripts/start_cluster.sh
bash scripts/start_cluster.sh stop
```

### 手动逐个启动

```powershell
go build -o medtrust-node.exe ./cmd/node/
.\medtrust-node.exe --config configs/node1.yaml
.\medtrust-node.exe --config configs/node2.yaml
.\medtrust-node.exe --config configs/node3.yaml
```

## 前端启动

```powershell
cd web
npm install
npm run dev
```

启动后访问：

- [http://localhost:3000](http://localhost:3000)

## 端口说明

| 节点 | RPC | API |
| --- | --- | --- |
| node-1 | 7001 | 8001 |
| node-2 | 7002 | 8002 |
| node-3 | 7003 | 8003 |

## 快速验证

### 查看节点状态

```powershell
curl http://127.0.0.1:8001/api/node/status
curl http://127.0.0.1:8002/api/node/status
curl http://127.0.0.1:8003/api/node/status
```

### 提交一条上链数据

当前版本要求通过医生录入、患者确认等门户流程提交病历。直接向底层 `/api/record/upload` 发送未签名载荷会被拒绝，这是防止绕过患者审批的预期行为。

### 查询区块账本

```powershell
curl "http://127.0.0.1:8001/api/records?limit=10"
```

## 关键说明

- 只有 Leader 节点可以处理写请求
- 前端会自动轮询三个节点状态
- 区块持久化后会写入 BoltDB，并在账本区域显示
- `configs/node*.yaml` 中的 API Key 是仅供本地演示的示例值，不适合互联网部署

## 常见问题

### 页面显示节点离线

通常是因为执行了：

```powershell
... stop
... reset
```

这会让后端全部停掉。正确恢复方式：

```powershell
powershell -ExecutionPolicy Bypass -File .\scripts\start_cluster.ps1 reset
powershell -ExecutionPolicy Bypass -File .\scripts\start_cluster.ps1 start
```

### 提交了数据但账本不显示

先检查：

```powershell
curl "http://127.0.0.1:8001/api/records?limit=10"
```

如果接口里能看到数据，说明后端已写入，刷新前端即可。

## 相关文档

- [门户网站运行说明](./docs/门户网站运行说明.md)

