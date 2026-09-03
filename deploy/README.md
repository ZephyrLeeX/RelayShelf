# RelayShelf 生产部署包

本部署包用于安装已冻结的生产架构：Debian 13 上以 rootful 系统模式运行
Podman Quadlet，包含一个 RelayShelf 容器和一个 PostgreSQL 17 容器；
PostgreSQL 数据存放在虚拟机本地磁盘，业务存储通过宿主机挂载 NFSv4。
OpenWrt nginx 负责外部 TLS 终止。Docker Compose 不是生产部署权威。

## 前置条件

- 具有固定 LAN 地址的 Debian 13 虚拟机，使用 cgroup v2、systemd、
  Podman/Quadlet >= 5.2.0（Debian 13 提供 5.4.2），并安装 `findmnt`、
  NFS 客户端工具、`openssl` 和 `curl`；内存至少为 6 GiB。
- 使用精确、完全限定且带 SemVer 标签的 RelayShelf 镜像，例如
  `<registry>/relayshelf:0.12.0`。拒绝 `latest` 和未限定的镜像名称。
- 一个 NFSv4 导出，其 RelayShelf 目录允许 UID/GID 65532 写入。
- 一个 OpenWrt nginx TLS 端点，以及仅允许 OpenWrt 代理/LAN 管理边界
  访问虚拟机 TCP 8080 的防火墙规则。本部署包绝不会发布 PostgreSQL 5432。
- 每次升级前均须准备最新的 PostgreSQL 备份和 `APP_ENCRYPTION_KEY` 备份。

检查宿主机运行时：

```bash
podman --version
podman info --format '{{.Host.CgroupsVersion}}'
test "$(podman info --format '{{.Host.CgroupsVersion}}')" = v2
sudo ./scripts/verify.sh
```

生产 Quadlet 使用的 `NetworkAlias=` 语法要求最低版本为 5.2.0；
`Notify=healthy` 还要求版本不低于 5.0.0。验证脚本会报告生成器版本；
若不满足该版本契约，会在解析单元前直接失败。

应用健康检查使用 JSON exec 形式 `HealthCmd=["/relayshelf","healthcheck"]`。
Podman 会把普通字符串形式的多词健康检查交给 `/bin/sh -c`，而生产镜像是
不含 shell 的 distroless 镜像。`Notify=healthy` 会等待上述 Podman 健康检查
成功后才向 systemd 发出启动完成通知；验证脚本会检查 5.4.2 生成器最终产生的
`--health-cmd` 参数仍是 JSON 数组，防止渲染或转义退化为 shell 形式。

生产网络是 RelayShelf 专用的 rootful Podman managed bridge，但不是 Podman
`--internal` 网络。Podman 5.4.2 的 rootful 端口转发不会为 internal 网络建立
可工作的 published-port 数据路径，因此 `Internal=true` 与应用的
`PublishPort=<debian-lan-ip>:8080:8080` 不兼容。网络隔离边界由专用 bridge、
PostgreSQL 完全没有 `PublishPort`、应用只在 Debian LAN IP 发布 8080，以及
宿主机防火墙只允许 OpenWrt/管理边界访问共同构成。不要把“专用 bridge”与
Podman `--internal` 的禁止外部访问语义混为一谈。

## 目录布局与所有权

```text
/etc/containers/systemd/           root:root 0755  生成的 Quadlet
/etc/relayshelf/relayshelf.env     root:root 0600  应用配置和密钥
/etc/relayshelf/postgres.env       root:root 0600  数据库初始化密钥
/var/lib/relayshelf/postgres/      999:999   0700  虚拟机本地 PostgreSQL 数据
/var/lib/relayshelf/staging/       65532     0750  虚拟机本地上传暂存区
/mnt/relayshelf/                   65532     NAS   宿主机 NFSv4 挂载点
/usr/local/bin/relayshelf-upgrade  root:root 0755  固定版本升级入口
/usr/local/libexec/relayshelf-host-storage-check
```

PostgreSQL 数据绝不能放在 NFS 上。应用 Quadlet 还会强制镜像以
UID/GID 65532 运行，并使用只读根文件系统。

## 配置宿主机 NFS 挂载

安装 Debian NFS 客户端、创建挂载点，并在 `/etc/fstab` 中添加由运维人员
按实际环境填写的条目。请替换所有占位符：

```bash
sudo apt-get install podman nfs-common util-linux openssl curl
sudo install -d -m 0750 /mnt/relayshelf
```

```fstab
<nas-host>:/<nas-export> /mnt/relayshelf nfs4 rw,hard,_netdev,nofail,x-systemd.mount-timeout=90s,timeo=600,retrans=2 0 0
```

随后挂载并验证。`findmnt` 必须报告 `nfs` 或 `nfs4`；普通本地目录会被
明确拒绝。

```bash
sudo systemctl daemon-reload
sudo mount /mnt/relayshelf
findmnt --mountpoint /mnt/relayshelf --types nfs,nfs4
sudo chown 65532:65532 /mnt/relayshelf
sudo ./libexec/relayshelf-host-storage-check /mnt/relayshelf
```

如果导出使用 root-squash，请使用 NAS 的所有权或 ACL 机制，而不是
`chown`。最终要求是数字 UID/GID 65532 对该目录具有读、写和遍历权限。

### NFS stale file handle 自动恢复

`findmnt` 仍显示挂载并不代表 NFS 可访问。NAS 升级、重启或重新导出文件系统后，
旧客户端句柄可能让 `stat /mnt/relayshelf` 返回 `Stale file handle`，同时应用报告
`STORAGE_UNAVAILABLE`。安装和升级流程会安装并启用
`relayshelf-storage-recovery.timer`，每分钟进行一次只读 `stat`；日常检查不会创建
文件，也不会调用应用的完整 storage check。

只有连续两次（间隔 3 秒）明确出现英文 `Stale file handle`，且当前挂载为
`nfs`/`nfs4`、source 与 `/etc/fstab` 中该 target 的 source 完全一致，才会进入恢复。
恢复顺序为：停止应用、限时普通卸载、必要时限时 `umount -f`（永不使用 `-l`）、
重新挂载、验证挂载身份和真实访问、以 UID/GID 65532 在 `.commit-tmp` 做
write/read/delete、启动应用并执行 `/relayshelf storage check`。网络超时、权限错误、
ENOSPC/EDQUOT、普通 I/O 错误、挂载缺失或 source 不匹配都只记录诊断，不会卸载。

恢复与正式升级共同竞争 `/run/relayshelf/operation.lock`，因此 watchdog 不会在
升级 stop/start 应用期间进入 destructive recovery，升级也不会在 recovery 正在
卸载/重挂 NFS 时启动。恢复自身从 recovery 开始执行 5 分钟冷却，避免失败风暴。
卸载、挂载、UID 探针或应用 storage check 失败时，
恢复返回失败；一旦应用已停止，失败路径会保持应用停止，避免在未验证存储上继续
服务或形成 stop/start 循环，需运维人员诊断后手动启动。

常用诊断和控制命令：

```bash
findmnt /mnt/relayshelf
stat /mnt/relayshelf
systemctl status relayshelf-storage-recovery.timer
journalctl -u relayshelf-storage-recovery.service
sudo systemctl start relayshelf-storage-recovery.service
sudo podman exec relayshelf-app /relayshelf storage check

# 临时禁用/恢复 watchdog
sudo systemctl disable --now relayshelf-storage-recovery.timer
sudo systemctl enable --now relayshelf-storage-recovery.timer
```

若需人工恢复，先禁用 timer，然后沿用相同安全顺序；必须核对 fstab source，停止
应用后依次普通卸载、必要时 `umount -f`、重新挂载、以应用 UID 验证写入，最后启动
应用并运行原生 storage check。禁止使用 lazy unmount，也不要把 hard mount 改为
soft mount。

## 密钥与环境配置

创建仅供当前操作使用的私有副本。安装程序会拒绝权限模式不是 0600，
或仍包含 `<placeholders>` 的文件。

```bash
install -m 0600 env/postgres.env.example ./postgres.env
install -m 0600 env/relayshelf.env.example ./relayshelf.env
openssl rand -base64 32
openssl rand -base64 32
```

单独生成一个高熵数据库密码。将密码写入 `DATABASE_URL` 时必须进行
URL 编码。主机名必须保持为 `relayshelf-postgres`，该名称仅在 Podman
私有网络中解析。设置 `PUBLIC_ORIGIN=https://<public-domain>`，并将
`TRUSTED_PROXIES` 严格限制为 OpenWrt 代理地址或 CIDR。不要提高示例中
针对 J4125 参考环境冻结的并发默认值。

在设置 `RELAYSHELF_KEY_BACKUP_CONFIRMED=yes` 前，必须先将
`APP_ENCRYPTION_KEY` 备份到虚拟机之外。只有下文所述的真实检查通过后，
才能设置 TLS/代理证明项。安装程序永远不会自动设置这些证明项。

## 全新安装

安装脚本采用故障安全设计：不会覆盖任何现有的生产环境文件或 Quadlet
文件。脚本会拉取精确镜像、验证配置、启动健康的 PostgreSQL、运行内嵌
迁移、以应用 UID 检查 NFS 存储、启动 RelayShelf，并检查就绪状态。

```bash
sudo ./scripts/install.sh \
  --image ghcr.io/zephyrleex/relayshelf:<version> \
  --listen-address <debian-vm-ip> \
  --app-env ./relayshelf.env \
  --postgres-env ./postgres.env
```

安装必须从对应 GitHub Release 的 `relayshelf-deploy-<version>.tar.gz` 完成，并在
解压前核对同一 Release 中的 `.sha256`。安装成功后会以 `root:root 0755` 安装长期
入口 `/usr/local/bin/relayshelf-upgrade`；此后生产机不需要保留 RelayShelf Git
仓库，也不需要 `git`、Go、Node.js、pnpm 或 Make。

```bash
relayshelf-upgrade --help
sudo ./scripts/verify.sh --installed
```

常用生命周期命令：

```bash
sudo systemctl start relayshelf-postgres.service relayshelf-app.service
sudo systemctl stop relayshelf-app.service relayshelf-postgres.service
sudo systemctl status relayshelf-postgres.service relayshelf-app.service
sudo journalctl -u relayshelf-postgres.service -u relayshelf-app.service -f
sudo podman exec relayshelf-app /relayshelf version
sudo podman exec relayshelf-app /relayshelf healthcheck
curl --fail http://<debian-vm-ip>:8080/health/live
curl --fail http://<debian-vm-ip>:8080/health/ready
```

不要在这些命令中放入凭据。密钥只能从受保护的环境文件中读取。

## 首次管理员初始化

全新数据库迁移完成后不会自动生成账号。首次管理员必须由生产运维人员通过
容器内的本地 CLI 明确创建；该能力没有公网 HTTP endpoint，也不会由
`install.sh`、`upgrade.sh`、应用启动或 migration 自动触发。

`admin bootstrap` 只在 `users` 表完全为空时可用。只要存在任何用户行（包括
禁用用户或非管理员），命令就会拒绝执行；没有 `--force`、覆盖、密码重置或
权限提升入口。命令会先要求数据库 schema 与当前二进制完全兼容，再以单个
事务完成空表检查、首管理员创建和 system/bootstrap 审计。创建出的用户固定为
`ACTIVE` 且 `is_admin=true`。

以下是 v0.1.3 正式发布后的生产 runbook。**这些命令只在生产机执行，不得在
开发环境执行。** 首先确认 PostgreSQL 和 `APP_ENCRYPTION_KEY` 的异机备份均可用，
然后升级到精确发布镜像：

```bash
sudo ./deploy/scripts/upgrade.sh \
  --image ghcr.io/zephyrleex/relayshelf:0.1.3 \
  --backup-confirmed

sudo podman exec relayshelf-app /relayshelf version
sudo podman exec relayshelf-app /relayshelf healthcheck
```

随后使用交互式 TTY 创建首管理员。用户名会按正常用户创建规则规范化；密码和
确认密码由终端无回显读取，绝不能作为 argv、环境变量或日志内容传入：

```bash
sudo podman exec -it relayshelf-app \
  /relayshelf admin bootstrap \
  --username <username> \
  --display-name "<display-name>"
```

成功后通过正式 HTTPS Origin 正常登录，在 RelayShelf UI/API 中完成并确认 TOTP
注册，再继续 TLS、反向代理及其他资格验证。bootstrap 不会生成或显示 TOTP
secret，也不会设置 `RELAYSHELF_TLS_TERMINATION_CONFIRMED` 或
`RELAYSHELF_PROXY_CONFIG_CONFIRMED`。因此首管理员创建后、TOTP 尚未确认时，
`security check` 仍应以“active administrator has not confirmed TOTP”失败；完成
TOTP 且运维证明项真实满足后，才运行：

```bash
sudo podman exec relayshelf-app /relayshelf security check
```

若命令报告已有用户，禁止删除、修改或手工提升现有数据来重开 bootstrap；应按
正常的已认证管理员流程处理账号，或停止操作并调查数据库来源。若报告 schema
不兼容，应先使用既有升级/migration authority 修复版本关系，bootstrap 本身不会
执行 migration。

## OpenWrt nginx

将 `nginx/openwrt-relayshelf.conf` 复制到 OpenWrt nginx 配置中，并替换
`<public-domain>`、`<debian-vm-ip>` 和证书占位符。使用软件包常规的
OpenWrt 服务命令进行测试和重新加载。参考配置会：

- 终止 HTTPS 并负责 HSTS；
- 替换客户端转发请求头，而不是追加；
- 禁用私有 API 流量缓存；
- 禁用 SSE 响应缓冲并延长其超时时间；
- 对 8 MiB 上传分块禁用请求缓冲；
- 对 Range 下载禁用响应缓冲；
- 将请求体限制为 32 MiB，而不是 2 GiB。

重新加载后，通过 `https://<public-domain>` 验证登录/CSRF、Secure Cookie、
主机名/来源拒绝、登录速率限制、SSE、中断后恢复的分块上传、ETag/Range
下载和 Content-Disposition。只有全部通过后，运维人员才能设置 TLS/代理
证明项并运行 `relayshelf security check`。

## 升级

创建 PostgreSQL 备份，并确认虚拟机外的加密密钥备份有效，然后在交互式终端运行：

```bash
sudo relayshelf-upgrade 1.2.3
```

版本参数必须是明确的 SemVer，且不带 tag 使用的 `v`；不支持 `latest`、`stable`
或自动查询最新版本。命令固定从 GitHub Release `v1.2.3` 下载 deployment bundle
及 SHA256，校验归档路径、`RELEASE_SCHEMA`、版本、Git commit 和精确 GHCR image，
然后提示运维人员输入 `YES` 确认 PostgreSQL 与加密密钥备份。非交互调用默认拒绝。
`/run/relayshelf/upgrade.lock` 会让第二个 updater 立即失败，而不是并行或长时间等待。
稳定版之间会进行数值 SemVer downgrade 检查；复杂 prerelease 的迁移方向仍由候选
镜像现有的 `migrate status` 权威检查保护。

即使应用已运行目标版本，命令也会继续 reconciliation，因为 Quadlet、host helper、
recovery service/timer 和 updater 本身都是同一 release 的组成部分。下载、checksum、
archive 或 metadata 验证失败均发生在 bundle `upgrade.sh` 被调用之前，不会停止应用、
运行 migration 或修改 systemd。临时 bundle 仅放在私有的
`/tmp/relayshelf-upgrade.XXXXXX`，成功或失败都会清理。

bundle 内的 `scripts/upgrade.sh --image <exact-image> --backup-confirmed` 仍是生产预检、
migration、Quadlet、watchdog 和健康检查的唯一执行 authority。直接调用该脚本只作为
故障排查/内部运维接口，不是日常生产升级入口。

预检会检查 NFS 身份和能力、本地可用磁盘、精确的候选镜像及其元数据、
完整配置、数据库可达性和迁移方向，以及明确的备份确认。只有所有检查
通过后，脚本才会停止应用、运行候选二进制内嵌迁移、安装新镜像单元、
重新启动并检查就绪状态。若检测到 v0.1.1 的 `Internal=true` 网络，脚本还会：

1. 备份 app 和 network Quadlet；
2. 依次停止应用、PostgreSQL 和 network service；
3. 安装新 network authority，删除没有容器占用的旧 Podman network，并重建
   普通专用 bridge；
4. 确认新网络 `Internal=false`，再启动 PostgreSQL 并等待其 healthy；
5. 运行迁移、启动应用，并从宿主机请求已发布的 LAN 端口；
6. 确认 `podman port relayshelf-postgres` 为空。

Quadlet 不支持原地修改已有网络参数，所以仅执行 `daemon-reload` 不足以完成
此次升级。网络重建要求 PostgreSQL 短暂停机，但不会删除或移动
`/var/lib/relayshelf/postgres`，也不会触碰 `/mnt/relayshelf` 或环境文件。
如果新网络在数据库迁移前创建失败，脚本会尝试恢复旧 network authority 和
原服务；迁移失败后则保留已修复的网络、运行中的 PostgreSQL 和停止的旧应用，
避免不受支持的数据库降级。

迁移失败时，旧单元保持不变，应用保持停止。就绪检查失败会返回非零状态。
旧镜像不会被删除，之前的应用单元会保留为：

```text
/etc/containers/systemd/relayshelf-app.container.previous.<UTC timestamp>
/etc/containers/systemd/relayshelf.network.previous.<UTC timestamp>
```

## 回滚

RelayShelf 不支持自动降级数据库模式。如果新模式与旧二进制兼容，运维人员
可以恢复之前的 Quadlet、重新加载 systemd 并启动服务。否则必须将匹配的
PostgreSQL 备份与之前的 Quadlet/镜像一并恢复：

```bash
sudo install -m 0644 \
  /etc/containers/systemd/relayshelf-app.container.previous.<UTC-timestamp> \
  /etc/containers/systemd/relayshelf-app.container
sudo systemctl daemon-reload
sudo systemctl start relayshelf-app.service
sudo podman exec --env RELAYSHELF_HEALTHCHECK_URL=http://127.0.0.1:8080/health/ready \
  relayshelf-app /relayshelf healthcheck
```

故障响应期间，绝不能尝试手工执行 `psql` 模式补丁，也不能删除旧镜像。

不要在正常回滚中恢复 v0.1.1 的 internal network：它会重新破坏宿主机端口
转发。network 的 previous 副本只用于“新网络尚未建立、迁移尚未开始”时的
故障恢复。

## 发布元数据与部署包创建

发布工程必须提供真实 SemVer 和匹配的镜像标签；本部署包不会虚构
`v1.0.0`：

```bash
./scripts/build-release.sh <semver> <registry>/relayshelf:<semver> <output.tar.gz>
```

该脚本要求 Git 工作树干净，会注入版本、Git 提交和构建时间，验证运行中
二进制的输出，并将含 `RELEASE_SCHEMA=1` 的 `RELEASE-METADATA` 写入归档。

正式发布由 tag 驱动：

```bash
git tag v1.2.3
git push origin v1.2.3
```

Release workflow 首先以 reusable `CI` workflow 对该 tag commit 执行完整质量门，
随后先检查同 tag Release 的发布状态，再构建并验证
`ghcr.io/zephyrleex/relayshelf:1.2.3` 和 deployment bundle，生成标准
`sha256sum` 文件。只有精确 GHCR image push 成功后，才以 draft GitHub Release
上传 bundle 与 checksum；两个 asset 均可见后才发布 Release。发布 asset 失败会使
workflow 失败，draft 不会成为正常可见的生产 Release。生产升级始终绑定明确 tag、
明确 asset 与 release-pinned exact SemVer image，不读取 `main`、raw script 或 mutable
image tag。每个正式 SemVer tag 只允许发布一次：未完成的同 tag draft 可以在 workflow
重跑时恢复，但 Release 一旦 published，后续运行会在 build、GHCR push 和 asset upload
之前失败，不允许覆盖 Release assets 或重新 push 同版本镜像。已发布版本必须以新版本
修复，例如用 `v1.2.4` 修复 `v1.2.3`，不能重发 `v1.2.3`。

## 故障排查

- 缺少 `relayshelf-app.service`：运行 `./scripts/verify.sh`，然后检查
  `journalctl -u relayshelf-app.service` 和 Quadlet 生成器 dry-run 输出。
- NFS 预检失败：将 `findmnt --mountpoint /mnt/relayshelf` 与 `/etc/fstab`
  对照；绝不能创建本地后备目录绕过检查。
- PostgreSQL 不健康：检查其日志，以及 `/var/lib/relayshelf/postgres` 下的
  所有权和可用空间；5432 必须继续保持私有。
- 配置失败：使用受保护的环境文件运行精确候选镜像的 `config check`；
  该命令会隐去密钥值。
- 迁移失败：保持应用停止并保留日志；在任何重启之前诊断内嵌迁移失败。
- 就绪失败：live 表示进程存在，ready 还会检查数据库和模式。检查两个单元
  的日志，并使用候选镜像运行 `migrate status`。
- 存储失败：先运行宿主机身份检查，再在容器内运行
  `relayshelf storage check`；不要把空的本地挂载点当作业务存储。

## 参考环境资格验证

GitHub Actions 的 deployment job 运行在非特权容器内，不能可靠执行 rootful
Netavark/NAT 测试；Quadlet generator dry-run 也不等价于 runtime networking。
仓库因此提供一个必须在 Debian 13 rootful Podman 参考机运行的最小 smoke test：

```bash
sudo ./tests/podman-network-smoke.sh
```

它创建临时普通 bridge 和 BusyBox HTTP 容器，把容器 8080 发布到宿主机
`127.0.0.1:18080`，从宿主机请求并要求 HTTP 200，随后清理临时容器和网络。
生产升级后还须执行：

```bash
sudo podman network inspect --format '{{.Internal}}' relayshelf
curl --fail http://10.0.0.4:8080/health/live
curl --fail http://10.0.0.4:8080/health/ready
sudo podman exec relayshelf-app /relayshelf healthcheck
sudo podman port relayshelf-postgres
if sudo ss -ltnp | grep ':5432'; then
  echo 'FAIL: PostgreSQL exposed'
  exit 1
fi
```

第一条必须输出 `false`，两个 curl 必须返回 HTTP 200，容器 healthcheck 必须
PASS，`podman port relayshelf-postgres` 必须没有输出，`ss` 不得显示宿主机 5432
监听。还要从 OpenWrt 对两个 LAN URL 重复 curl 验证。

静态验证不能证明 Phase 12 退出门已通过。仍须在真实 Debian 13 虚拟机、
rootful Podman/systemd Quadlet、PostgreSQL 17 本地持久化、NFSv4 NAS 和
OpenWrt nginx 环境中验证冷启动、挂载失败安全、迁移、健康/就绪、SSE、
分块上传、Range 下载、重启和升级。在保留这些运行记录之前，其状态仍为
`NOT EXECUTED / REQUIRES REFERENCE ENVIRONMENT`，Phase 12 退出门仍未通过。
