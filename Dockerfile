# 第一阶段：构建阶段
FROM golang:1.23-alpine AS builder

# 设置工作目录
WORKDIR /app

# 复制 go.mod 和 go.sum 文件
COPY go.mod go.sum ./

# 设置Go代理并下载依赖
ENV GOPROXY=https://goproxy.cn,direct
RUN go mod download

# 复制项目源代码
COPY . .

# 编译Go应用，构建一个静态链接的二进制文件
# -w -s 标志用于减小二进制文件大小
# -ldflags="-extldflags=-static" 用于静态链接
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -ldflags="-w -s" -o mcp-link .

# 第二阶段：运行阶段
FROM alpine:latest

# 设置工作目录
WORKDIR /root/

# 从构建阶段复制编译好的二进制文件
COPY --from=builder /app/mcp-link .

# 复制必要的配置文件和资源
COPY --from=builder /app/assets ./assets
COPY --from=builder /app/examples ./examples
COPY --from=builder /app/openapi.yaml ./openapi.yaml

# 暴露应用程序的端口
EXPOSE 8080

# 运行应用程序
CMD ["./mcp-link", "serve", "--port", "8080", "--host", "0.0.0.0"]
