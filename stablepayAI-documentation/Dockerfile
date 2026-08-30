# 构建阶段
FROM node:20-alpine AS builder
WORKDIR /app
RUN apk add --no-cache unzip
COPY package*.json ./
RUN npm ci
COPY . .
# mintlify export 的 --output 是 zip 文件路径，不是目录
RUN npx mintlify export --output ./_site.zip \
    && unzip -q ./_site.zip -d ./_site \
    && test -f ./_site/index.html

# 运行阶段 - 轻量级 Nginx
FROM nginx:alpine
COPY --from=builder /app/_site/ /usr/share/nginx/html/docs/
COPY scripts/prefix-docs-paths.sh /tmp/prefix-docs-paths.sh
RUN sed -i 's/\r$//' /tmp/prefix-docs-paths.sh \
    && chmod +x /tmp/prefix-docs-paths.sh \
    && /tmp/prefix-docs-paths.sh /usr/share/nginx/html \
    && test -f /usr/share/nginx/html/docs/index.html \
    && nginx -t
COPY nginx.conf /etc/nginx/conf.d/default.conf
EXPOSE 80
