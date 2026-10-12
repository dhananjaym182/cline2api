FROM golang:1.26-alpine AS builder

WORKDIR /build
COPY go.mod ./
RUN go mod download 2>/dev/null || true
COPY . .
ARG APP_VERSION=dev
RUN CGO_ENABLED=0 go build -ldflags="-s -w -X main.appVersion=${APP_VERSION}" -o cline-proxy .

FROM alpine:3.21

RUN apk add --no-cache ca-certificates tzdata
WORKDIR /app
COPY --from=builder /build/cline-proxy .

EXPOSE 3457

VOLUME ["/app/data"]

ENV PORT=3457
ENV CLINE_PROXY_HOST=0.0.0.0
# 数据目录指向 VOLUME 挂载点，重建容器（不删卷）数据不丢
ENV CLINE2API_DATA_DIR=/app/data

ENTRYPOINT ["/app/cline-proxy"]
# 容器内必须监听 0.0.0.0，否则 -p 端口映射对外不可达
CMD ["-host", "0.0.0.0", "-port", "3457"]
