# 自定义 CLI 升级到 MinerU 4.1

`mineru parse` 通过本地 Doclib 排队，非 Flash 解析由 `MinerUApiParser` 连接 API Server 或 Router。
合并 4.1 后 SDK 已使用新的 Tasks 协议，CLI 命令及 Doclib 自己的 `/api/v1` 路由保持原有职责。

## HTTP 行为

- 创建使用 `POST /v1/tasks` JSON 文件清单，包含文件名、大小、SHA-256 和可选页范围。
- 缓存命中免传；未命中按 `files[].upload` 描述 PUT 原始文件，无需 complete。
- 根据配置的 API 基址与任务 ID 查询，保留反向代理路径前缀。
- 从成功文件的 `result_url` 下载结果，读取 ZIP 中的 Middle JSON；不请求 ModelJson。
- Doclib 先检查 `/v1/health` 声明的 `features.task_output_formats` 包含 `default`、
  `features.sources` 包含 `upload`，再查询 `/v1/tiers`。旧版服务不能仅凭档位查询成功被判为就绪。
  `mineru server status` 及解析失败信息会保留升级提示。

`mineru parse --no-wait`、`--wait` 和再次执行命令继续等待仍由 Doclib 管理，
不依赖独立的 `/v1/file_parse` 服务。解析服务地址应指向主 API Server 或 Router。

## 升级步骤

1. 使用 Python 3.11–3.14，按项目规范通过 uv 更新当前虚拟环境依赖。
2. 停收请求，等待旧任务完成并保存产物；先升级 worker，再同步升级 Router 和客户端。
3. 执行 `mineru server restart`，让已经运行的 Doclib 和托管解析子进程加载新代码。
4. 执行 `mineru server status --json`，确认目标服务健康及档位符合预期。
5. 使用 PDF 和原生文档验证解析、缓存命中、页范围与输出。

Doclib 保留自己的选档策略：PDF/图片默认选 Standard 或 Basic，Flash 需显式指定；
原生文档按既有规则使用 Flash。托管档位配置仍是 Basic、Standard、Advanced 上限，
独立服务新增的 `--tier auto` 不会自动改变已有 Doclib 配置。

硬件诊断可使用 `mineru-kit hardware info --json`；需要测速和推荐时使用
`mineru-kit hardware detect --json`，后者可能准备或下载模型。

完整 HTTP 变更见 [4.1 迁移说明](../../zh/reference/migration_4_1.md)。
