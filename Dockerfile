FROM stablepay-registry.cn-shanghai.cr.aliyuncs.com/stablepay-dev/node:20-alpine AS builder
WORKDIR /app
COPY package*.json ./
RUN npm ci
COPY . .
RUN npm run build

FROM stablepay-registry.cn-shanghai.cr.aliyuncs.com/stablepay-dev/nginx:1.27-alpine
COPY deploy/nginx.conf /etc/nginx/conf.d/default.conf
COPY --from=builder /app/dist /usr/share/nginx/html

EXPOSE 80
CMD ["nginx", "-g", "daemon off;"]