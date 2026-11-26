# 自托管 Atrace

准备 Go 1.27.3、Bun 1.4.2 和 Bash。在源码目录运行：

```bash
(cd client/web && bun install --frozen-lockfile && bun run build)
(cd server && mkdir -p build && CGO_ENABLED=0 go build -trimpath -o build/atrace-server .)
```

在同一个终端生成采集令牌，初始化主人密码并启动服务。请保存生成的采集令牌，供之后启动采集器时使用。

```bash
export ATRACE_COLLECT_TOKEN="$(bun -e 'console.log(require("node:crypto").randomBytes(32).toString("hex"))')"
read -rsp '主人密码（至少 8 字符）: ' owner_password
printf '\n'
printf '%s\n' "$owner_password" | ./server/build/atrace-server set-password
unset owner_password
./server/build/atrace-server &
atrace_pid=$!
```

默认地址是 `http://127.0.0.1:8183`，档案和媒体保存在当前目录的 `data/`。运行下面的命令导入合成样例：

```bash
bun scripts/import-demo.mjs
```

打开 `http://127.0.0.1:8183/login`，用主人密码登录。在「设置 → 人物」创建一个人物，把样例账号 `demo:user:reader` 关联给它，并把该人物放在首位。回到首页即可看到样例。网页终端在 `/terminal`。如需单独运行 CLI，可以执行 `bun client/cli/atrace --help`。

备份时保留档案数据库和媒体目录。停止服务用 `kill "$atrace_pid"`。

## 采集器

采集器用 `ATRACE_COLLECT_TOKEN` 提交数据。浏览器采集示例依赖已安装的 Ego Browser 和你自己的已登录会话，可用 `ATRACE_EGO` 指定可执行文件。采集器目录和提交协议见[采集器说明](../client/cli/collector-examples/AGENTS.md)。

## 构建镜像和桌面 App

```bash
docker build -f server/Dockerfile -t atrace:local .
```

镜像在容器内监听 `8183`。挂载可写的 `/data` 以保存档案，初始化时把 `set-password` 传给容器，运行时设置 `ATRACE_COLLECT_TOKEN`。

在 Apple Silicon Mac 上运行 `bash client/app/build.sh` 可构建桌面 App。自建发行方可以通过 `ATRACE_UPDATE_URL` 和 `ATRACE_UPDATE_PUBLIC_KEY` 配置自己的更新源。
