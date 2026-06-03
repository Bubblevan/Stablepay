# 构建阶段
FROM node:20-alpine AS builder
WORKDIR /app
COPY package*.json ./
RUN npm ci
COPY . .
RUN npx mintlify export --output ./_site

# 运行阶段 - 轻量级 Nginx
FROM nginx:alpine
COPY --from=builder /app/_site /usr/share/nginx/html/docs
COPY nginx.conf /etc/nginx/conf.d/default.conf
EXPOSE 80
