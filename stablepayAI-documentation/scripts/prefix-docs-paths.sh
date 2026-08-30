#!/bin/sh
# Mintlify export 按站点根路径生成；部署在 /docs/ 子路径时需改写路径。
# 注意：禁止在 .js 里做站内路由替换（会把 "/" 等字符串误改，导致 Next 运行时崩溃）。
set -e
ROOT="${1:-/usr/share/nginx/html}"
DOCS="${ROOT}/docs"

if [ ! -d "$DOCS" ]; then
  echo "ERROR: docs dir not found: $DOCS" >&2
  exit 1
fi

# 去掉 Windows CRLF，避免容器内 /bin/sh^M 或 sed 异常
sed -i 's/\r$//' "$0" 2>/dev/null || true

prefix_assets() {
  sed -i \
    -e 's|href="/docs/|href="/__DOCS__/|g' \
    -e 's|src="/docs/|src="/__DOCS__/|g' \
    -e 's|action="/docs/|action="/__DOCS__/|g' \
    -e 's|href="/__DOCS__/|href="/docs/|g' \
    -e 's|src="/__DOCS__/|src="/docs/|g' \
    -e 's|action="/__DOCS__/|action="/docs/|g' \
    -e 's|"/_next/|"/docs/_next/|g' \
    -e 's|"/_mintlify/|"/docs/_mintlify/|g' \
    -e 's|"/favicons/|"/docs/favicons/|g' \
    -e 's|"/logo/|"/docs/logo/|g' \
    -e 's|"/favicon.svg|"/docs/favicon.svg|g' \
    -e 's|"/sitemap.xml|"/docs/sitemap.xml|g' \
    -e 's|"/llms.txt|"/docs/llms.txt|g' \
    -e 's|"/llms-full.txt|"/docs/llms-full.txt|g' \
    -e 's|"/skill.md|"/docs/skill.md|g' \
    "$1"
}

# 1) HTML：静态资源路径
find "$DOCS" -type f -name '*.html' | while read -r f; do
  prefix_assets "$f"
done

# 2) JS/CSS：仅改 _next / _mintlify 等资源路径（不改站内路由）
find "$DOCS" -type f \( -name '*.js' -o -name '*.css' \) | while read -r f; do
  sed -i \
    -e 's|"/_next/|"/docs/_next/|g' \
    -e 's|"/_mintlify/|"/docs/_mintlify/|g' \
    -e "s|'/_next/|'/docs/_next/|g" \
    -e "s|'/_mintlify/|'/docs/_mintlify/|g" \
    "$f"
done

# 3) 收集文档路由（仅用于 HTML 链接）
ROUTES_FILE="$(mktemp)"
find "$DOCS" -name 'index.html' | while read -r f; do
  rel="${f#$DOCS}"
  rel="${rel%/index.html}"
  [ -z "$rel" ] && continue
  printf '%s\n' "/${rel}"
done | awk '{ print length, $0 }' | sort -rn | cut -d' ' -f2- > "$ROUTES_FILE"

# 4) HTML：站内导航链接（不处理根路径 "/"，避免误伤）
find "$DOCS" -type f -name '*.html' | while read -r f; do
  sed -i \
    -e 's|data-current-path="/"|data-current-path="/docs"|g' \
    -e 's|href="/"|href="/docs/"|g' \
    -e "s|href='/'|href='/docs/'|g" \
    "$f"

  while IFS= read -r route; do
    [ -z "$route" ] && continue
    case "$route" in
      /_next|/_mintlify|/favicons|/logo) continue ;;
    esac

    sed -i \
      -e "s|\"/docs${route}\"|\"/__ROUTE__${route}\"|g" \
      -e "s|'/docs${route}'|'/__ROUTE__${route}'|g" \
      -e "s|\"${route}\"|\"/docs${route}\"|g" \
      -e "s|\"${route}#|\"/docs${route}#|g" \
      -e "s|'${route}'|'/docs${route}'|g" \
      -e "s|'${route}#|'/docs${route}#|g" \
      -e "s|\"/__ROUTE__${route}\"|\"/docs${route}\"|g" \
      -e "s|'/__ROUTE__${route}'|'/docs${route}'|g" \
      "$f"
  done < "$ROUTES_FILE"
done

rm -f "$ROUTES_FILE"

# 5) 修正可能出现的双前缀，以及中文语言根路径
find "$DOCS" -type f -name '*.html' | while read -r f; do
  sed -i \
    -e 's|/docs/docs/|/docs/|g' \
    -e 's|data-current-path="/docs/docs"|data-current-path="/docs"|g' \
    -e 's|href="/docs/zh/"|href="/docs/zh/index/"|g' \
    -e 's|href="/docs/zh"|href="/docs/zh/index/"|g' \
    "$f"
done
