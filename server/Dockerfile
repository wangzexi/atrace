FROM oven/bun:1.4.2 AS web
WORKDIR /src/client/web
COPY client/web/package.json client/web/bun.lock ./
RUN bun install --frozen-lockfile
COPY client/web/ ./
RUN bun run --bun build

FROM golang:1.27.3-alpine AS server
WORKDIR /src/server
COPY server/go.mod server/go.sum ./
RUN go mod download
COPY server/ ./
RUN CGO_ENABLED=0 go build -trimpath -o /atrace-server .

FROM scratch AS runtime
ENV ATRACE_ADDR=0.0.0.0:8183 ATRACE_WEB_BUILD=/web ATRACE_DATABASE=/data/atrace.db ATRACE_FILES=/data/files
USER 1000:1000
EXPOSE 8183
ENTRYPOINT ["/atrace-server"]

# CI packages the exact binary and web assets that passed behavior tests.
FROM runtime AS package
COPY atrace-server /atrace-server
COPY web /web

# The default target still supports building directly from source.
FROM runtime AS production
COPY --from=server /atrace-server /atrace-server
COPY --from=web /src/client/web/build /web
