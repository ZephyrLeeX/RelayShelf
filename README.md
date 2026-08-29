# RelayShelf

**跨设备内容中转与存储中心**  
Cross-device content relay and storage.

RelayShelf 是一个面向个人/家庭使用的轻量级跨设备内容中转与存储系统，用于在 Windows、Linux、Android、iPhone、iPad 等设备之间快速保存、发送和取用文本、命令、链接、图片与文件。

> 当前状态：Phase 12 deployment/release engineering。Phase 11 exit gate 已由
> Phase 12 authority handoff 记录为通过。

## 核心定位

- 不是聊天软件
- 不是传统目录式网盘
- 不是同步盘
- 提供 Temporary / Permanent 两种内容生命周期
- 支持跨用户直接发送与转发
- 支持大文件分片上传、断点续传和服务端 SHA-256 全局去重
- 支持全文搜索、标签、收藏、回收站和敏感正文加密
- 以低资源占用和家庭自托管为主要设计目标

## 计划技术栈

- Frontend: Vue 3 + TypeScript + Vite
- Backend: Go + `net/http` + chi
- Database: PostgreSQL
- Search: PostgreSQL + `pg_trgm`
- Deployment: Podman Quadlet
- File storage: NFSv4-backed filesystem storage

## 文档

核心设计文档将维护在 `docs/`：

- `docs/PRD.md`
- `docs/ARCHITECTURE.md`
- `docs/DATA_MODEL.md`
- `docs/IMPLEMENTATION_PLAN.md`

Production operator bundle、Podman Quadlet、升级流程和 OpenWrt nginx reference
位于 [`deploy/`](deploy/README.md)。

## Branding

品牌与程序图标资源位于 `assets/brand/` 和 `web/public/`。
