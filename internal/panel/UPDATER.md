# 面板一键更新（宿主机侧 wb-panel-updater）

面板自己**不能**更新自己：panel 进程跑在容器里，uid 10001，没有 git、没有 docker CLI、
没有 docker.sock。所以更新动作（拉源码 → 重建镜像 → 重启容器）由一个**宿主机侧常驻服务**
代做，面板上只多一个「版本更新」页签负责展示与触发。

## 组成

| 部分 | 位置 | 作用 |
|---|---|---|
| 更新服务 | `/usr/local/bin/wb-panel-updater` | 常驻宿主机，暴露带令牌的 HTTP 面 |
| 运行配置 | `/etc/wb-panel-updater.json` (0600 root) | 仓库/分支/令牌/容器名 |
| 容器 | `wb-panel-updater`（`--restart unless-stopped`） | 承载上面的服务 |
| 面板配置 | `config.json` 的 `updater` 段 | 面板侧只知道地址与令牌 |
| 面板页签 | `#update`（「版本更新」） | 显示版本、检查、一键更新、看日志 |

## HTTP 面

全部要求 `X-WB-Token`（`/healthz` 除外）：

```
GET  /healthz   存活探测（无令牌）
GET  /status    当前版本 / 未提交改动数 / 上次任务 / 日志行数
GET  /check     真打一次 git fetch，返回 behind / updatable
POST /update    启动更新（异步，同一时刻只允许一个任务）
POST /update?force=1  允许丢弃工作区未提交改动（需 allow_force=true）
GET  /log?n=200 任务日志尾部
```

## 更新做的事

1. `git fetch --prune origin <branch>`；本地 HEAD 已等于远端则**直接返回**（不重建、不重启）
2. 工作区有未提交改动 → **默认拒绝**（避免 `git reset --hard` 抹掉你的工作）；`force=1` 才丢
3. `git checkout -f <branch>` + `git reset --hard origin/<branch>`
4. `docker compose build`
5. `docker compose up -d --force-recreate`
6. 宿主机侧探测 `http://127.0.0.1:7863/healthz` 直到就绪（最多 90s），然后记结果

## 常用操作

```bash
# 看状态（把 <TOKEN> 换成 /etc/wb-panel-updater.json 里的 token）
curl -s -H "X-WB-Token: <TOKEN>" http://127.0.0.1:13590/status | python3 -m json.tool

# 手动触发一次更新
curl -s -X POST -H "X-WB-Token: <TOKEN>" http://127.0.0.1:13590/update

# 看日志
tail -f /var/log/wb-panel-updater.log

# 重启服务（改完 /etc/wb-panel-updater.json 后）
docker restart wb-panel-updater
```

## 安全边界

- 服务端固定目标：仓库与分支来自 `/etc/wb-panel-updater.json`，**不接受客户端传入**命令、路径或 ref。
- 令牌鉴权：`X-WB-Token`（常量时间比较）。监听 `0.0.0.0:13590`，同宿主机上其他容器可达，
  所以令牌必须当成密码看待；泄漏了就用面板改 `updater.token` 并同步改 `/etc/wb-panel-updater.json`。
- 面板侧 `/panel/api/updater/*` 全部走面板鉴权（主密钥），分发出去的 `sk-` 密钥**不能**触发更新。
- 更新会把工作区重置到远端分支：**未提交的改动会丢**，这是默认拒绝的原因。

## 换机 / 重装

1. 编译：`CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o wb-panel-updater .`
2. 装二进制到 `/usr/local/bin/wb-panel-updater`
3. 写 `/etc/wb-panel-updater.json`（0600 root）
4. 起容器（容器里已有 git / docker CLI / docker-compose）：
   ```bash
   docker run -d --name wb-panel-updater --restart unless-stopped --user 0:0 -p 13590:13590 \
     -v /usr/local/bin/wb-panel-updater:/usr/local/bin/wb-panel-updater:ro \
     -v /etc/wb-panel-updater.json:/etc/wb-panel-updater.json:ro \
     -v /etc/localtime:/etc/localtime:ro \
     -v /var/run/docker.sock:/var/run/docker.sock \
     -v <仓库绝对路径>:<仓库绝对路径> \
     --entrypoint /usr/local/bin/wb-panel-updater wb-panel-updater:1.2.0 -config /etc/wb-panel-updater.json
   ```
   ⚠️ 容器里的 `/usr/bin/docker` 必须是**装进镜像的那份 glibc 版 CLI**（宿主机的 `/usr/bin/docker`
   是 glibc 二进制，Alpine 的 musl 加载不了，mount 进去会报 `not found`）：
   `ld-linux-x86-64.so.2` + `libc.so.6` + `docker` + `/usr/libexec/docker/cli-plugins/docker-compose`。
5. 面板 `config.json` 的 `updater` 段填地址与同一令牌（可在面板上改，下一个请求即生效）。

## 故障排查

| 现象 | 原因 / 处理 |
|---|---|
| 面板页签显示「不可达」 | 容器没跑：`docker ps \| grep wb-panel-updater`；端口被占：`-p` 换一个并同步改 `updater.url` |
| 面板页签显示「未配置更新服务」 | `config.json` 里没有 `updater.url` |
| `/check` 报 git fetch 失败 | 容器内没网 / 仓库 remote 变了 / 分支名写错 |
| `docker compose build` 失败 | 停在旧镜像，服务不受影响；看 `/var/log/wb-panel-updater.log` |
| 显示「已是最新」但版本没变 | 面板显示的是**源码 commit**；改了源码没提交推送 → 远端没新东西 |
