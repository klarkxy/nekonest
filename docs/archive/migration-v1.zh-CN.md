> [English](./migration-v1.md) | 简体中文
>
> **已归档。** 本文只适用于尚未发生的 v0.1 → v1.0 切割。不要对现行 **v0.2** 乐园执行。

# 从 v0.1 迁移到 v1.0

这是一次**破坏性**升级，不支持长期混跑协议。迁移后：

- Daemon 的 **设备 ID 与 token 哈希**保留（主机仍注册）。
- 服务器侧明文 **消息、prompt、配对码、推送订阅、手机身份、密钥包、附件** 在备份后清除。
- 手机必须 **重新输入 admin secret、签发 phone token 并重新配对**。
- 家用机上的 **原生 agent store 不会被迁移改动**。

## 前置条件

1. 停止乐园服务器及所有会写入该库的 daemon。
2. 记录 `NEKONEST_ADMIN_SECRET` / 旧名 `NEKONEST_PHONE_SECRET` 与 bootstrap token。
3. 为 `data/` 全量拷贝预留磁盘。

## 步骤

```bash
./nekonest-server -migrate-v1 -data ./data -backup ./data-backup-v1
```

命令会：

1. 将 `nekonest.db`（及可能的 WAL/SHM）和 `attachments/` 拷到 `-backup`。
2. 写入备份库的 `nekonest.db.sha256`。
3. 打开线上库，做增量 schema，再删除上述明文内容表。
4. 清空线上 attachments 目录（可用备份恢复）。
5. 在同一事务中原子持久化 `transport_mode=sealed`。

然后升级二进制（server、daemon、PWA）。不要断言 `open`：迁移后的数据库已经是 sealed。Server 可不设模式变量，或显式断言持久化值：

```bash
# 可选断言；不匹配会拒绝启动。
export NEKONEST_TRANSPORT_MODE=sealed
export NEKONEST_ADMIN_SECRET=...   # 原 NEKONEST_PHONE_SECRET
export NEKONEST_BOOTSTRAP_TOKEN=...
```

启动 Server；让每个 Daemon 注册/更新，使 `config.json` 持久化 sealed 模式；运行 `nekonest-daemon -doctor`，生成新配对码并让手机重配。未执行本迁移的已有 open 乐园仍保持 open；只改环境变量会被明确拒绝。

## 回滚

仅在仍使用 **v0.1** 二进制时，可将备份树覆盖回 `data/`。v1 客户端无法使用旧协议。已清除的乐园侧明文不会自动导入密封历史；对话内容应从主机 **原生 agent store** 恢复。

## 威胁模型摘要（迁移后）

- 密封模式：VPS 存密文；仍可能看到设备 ID、session ID、时间戳、大小、连接元数据。
- 开放模式：VPS 可读应用明文——仅管理员显式配置。
- 手机密钥丢失需重新配对；历史从原生 store 重建，不能靠旧 VPS 密文恢复。
