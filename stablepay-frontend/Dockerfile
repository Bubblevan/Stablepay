FROM stablepay-registry.cn-shanghai.cr.aliyuncs.com/stablepay-dev/node:20-alpine AS builder
WORKDIR /app
COPY package*.json ./
RUN npm ci
COPY . .
RUN npm run build

FROM stablepay-registry.cn-shanghai.cr.aliyuncs.com/stablepay-dev/nginx:1.27-alpine
COPY deploy/nginx.conf /etc/nginx/conf.d/default.conf
COPY --from=builder /app/dist /usr/share/nginx/html

# 构建产物为空时，SPA 的 try_files 会在运行时产生内部重定向死循环；探活也会间接失败
RUN test -f /usr/share/nginx/html/index.html || (echo "ERROR: dist missing index.html" && exit 1) \
    && nginx -t

EXPOSE 80
CMD ["nginx", "-g", "daemon off;"]