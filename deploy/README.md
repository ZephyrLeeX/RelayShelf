# RelayShelf 生产部署包

本部署包用于安装已冻结的生产架构：Debian 13 上以 rootful 系统模式运行
Podman Quadlet，包含一个 RelayShelf 容器和一个 PostgreSQL 17 容器；
PostgreSQL 数据存放在虚拟机本地磁盘，业务存储通过宿主机挂载 NFSv4。
OpenWrt nginx 负责外部 TLS 终止。Docker Compose 不是生产部署权威。

## 前置条件

- 具有固定 LAN 地址的 Debian 13 虚拟机，使用 cgroup v2、systemd、
  Podman/Quadlet >= 5.2.0（Debian 13 提供 5.4.2），并安装 `findmnt`、
  NFS 客户端工具和 `openssl`；内存至少为 6 GiB。
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

## 目录布局与所有权

```text
/etc/containers/systemd/           root:root 0755  生成的 Quadlet
/etc/relayshelf/relayshelf.env     root:root 0600  应用配置和密钥
/etc/relayshelf/postgres.env       root:root 0600  数据库初始化密钥
/var/lib/relayshelf/postgres/      999:999   0700  虚拟机本地 PostgreSQL 数据
/var/lib/relayshelf/staging/       65532     0750  虚拟机本地上传暂存区
/mnt/relayshelf/                   65532     NAS   宿主机 NFSv4 挂载点
/usr/local/libexec/relayshelf-host-storage-check
```

PostgreSQL 数据绝不能放在 NFS 上。应用 Quadlet 还会强制镜像以
UID/GID 65532 运行，并使用只读根文件系统。

## 配置宿主机 NFS 挂载

安装 Debian NFS 客户端、创建挂载点，并在 `/etc/fstab` 中添加由运维人员
按实际环境填写的条目。请替换所有占位符：

```bash
sudo apt-get install podman nfs-common util-linux openssl
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
  --image <registry>/relayshelf:<version> \
  --listen-address <debian-vm-ip> \
  --app-env ./relayshelf.env \
  --postgres-env ./postgres.env
```

常用生命周期命令：

```bash
sudo systemctl start relayshelf-postgres.service relayshelf-app.service
sudo systemctl stop relayshelf-app.service relayshelf-postgres.service
sudo systemctl status relayshelf-postgres.service relayshelf-app.service
sudo journalctl -u relayshelf-postgres.service -u relayshelf-app.service -f
sudo podman exec relayshelf-app /relayshelf version
sudo podman exec relayshelf-app /relayshelf healthcheck
curl --fail http://127.0.0.1:8080/health/live
curl --fail http://127.0.0.1:8080/health/ready
```

不要在这些命令中放入凭据。密钥只能从受保护的环境文件中读取。

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

创建 PostgreSQL 备份，并确认虚拟机外的加密密钥备份有效，然后运行：

```bash
sudo ./scripts/upgrade.sh \
  --image <registry>/relayshelf:<new-version> \
  --backup-confirmed
```

预检会检查 NFS 身份和能力、本地可用磁盘、精确的候选镜像及其元数据、
完整配置、数据库可达性和迁移方向，以及明确的备份确认。只有所有检查
通过后，脚本才会停止应用、运行候选二进制内嵌迁移、安装新镜像单元、
重新启动并检查就绪状态。

迁移失败时，旧单元保持不变，应用保持停止。就绪检查失败会返回非零状态。
旧镜像不会被删除，之前的应用单元会保留为：

```text
/etc/containers/systemd/relayshelf-app.container.previous.<UTC timestamp>
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

## 发布元数据与部署包创建

发布工程必须提供真实 SemVer 和匹配的镜像标签；本部署包不会虚构
`v1.0.0`：

```bash
./scripts/build-release.sh <semver> <registry>/relayshelf:<semver> <output.tar.gz>
```

该脚本要求 Git 工作树干净，会注入版本、Git 提交和构建时间，验证运行中
二进制的输出，并将 `RELEASE-METADATA` 写入归档。只有镜像仓库、签名和
保留策略获批后，发布流程才应推送该精确镜像。

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

静态验证不能证明 Phase 12 退出门已通过。仍须在真实 Debian 13 虚拟机、
rootful Podman/systemd Quadlet、PostgreSQL 17 本地持久化、NFSv4 NAS 和
OpenWrt nginx 环境中验证冷启动、挂载失败安全、迁移、健康/就绪、SSE、
分块上传、Range 下载、重启和升级。在保留这些运行记录之前，其状态仍为
`NOT EXECUTED / REQUIRES REFERENCE ENVIRONMENT`，Phase 12 退出门仍未通过。
