#!/bin/bash
# Run as root after SCP to /root/stablepay-upload. No builds or package installs.
set -euo pipefail
release_id=${1:?release id required}
public_host=${2:?HTTPS hostname required}
[[ "$release_id" =~ ^[0-9]{8}-[0-9]{6}-linux-(amd64|arm64)$ ]] || exit 2
[[ "$public_host" =~ ^[a-zA-Z0-9][a-zA-Z0-9.-]+[a-zA-Z0-9]$ ]] || exit 2
[[ $(id -u) -eq 0 ]] || exit 2
available_kb=$(awk '/MemAvailable:/{print $2}' /proc/meminfo)
available_disk_kb=$(df -Pk /opt | awk 'NR==2 {print $4}')
(( available_kb >= 524288 && available_disk_kb >= 512000 )) || { echo 'Insufficient spare memory or disk; deployment aborted'; exit 1; }
upload=/root/stablepay-upload
cd "$upload"
sha256sum -c "$release_id.tar.gz.sha256"
if ! id stablepay-mvp >/dev/null 2>&1; then useradd --system --user-group --home-dir /nonexistent --shell /usr/sbin/nologin stablepay-mvp; fi
install -d -m 0755 /opt/stablepay-mvp/releases /usr/local/libexec
install -d -m 0750 -o root -g stablepay-mvp /etc/stablepay-mvp /etc/stablepay-mvp/keys
install -d -m 0700 -o stablepay-mvp -g stablepay-mvp /var/lib/stablepay-mvp/payment /var/lib/stablepay-mvp/runtime
tar -xzf "$release_id.tar.gz" -C /opt/stablepay-mvp/releases
release=/opt/stablepay-mvp/releases/$release_id
chmod 0755 "$release/bin/payment-fiat" "$release/bin/procurement-runtime"
rollback_dir=$upload/rollback-$release_id
install -d -m 0700 "$rollback_dir"
if [[ -f /etc/stablepay-mvp/fiat.env ]]; then cp -a /etc/stablepay-mvp "$rollback_dir/config"; fi
install -d -m 0700 "$upload/private"
tar -xzf "$upload/config.tar.gz" -C "$upload/private"
for config in fiat runtime; do install -m 0600 -o root -g root "$upload/private/$config.env" "/etc/stablepay-mvp/$config.env"; done
for key in app-private alipay-public; do install -m 0640 -o root -g stablepay-mvp "$upload/private/$key.pem" "/etc/stablepay-mvp/keys/$key.pem"; done
install -m 0755 "$release/deploy/stablepay-mvp-egress" /usr/local/libexec/stablepay-mvp-egress
install -m 0644 "$release/deploy/"*.service "$release/deploy/"*.slice /etc/systemd/system/
if systemctl cat sni-gate.service >/dev/null 2>&1; then
    install -d -m 0755 /etc/systemd/system/sni-gate.service.d
    printf '[Service]\nExecStartPost=+/usr/local/libexec/stablepay-mvp-egress apply\n' > /etc/systemd/system/sni-gate.service.d/stablepay-mvp-egress.conf
fi
systemctl daemon-reload
systemctl enable --now stablepay-mvp-egress.service
previous_release=$(readlink -f /opt/stablepay-mvp/current 2>/dev/null || true)
ln -sfn "$release" /opt/stablepay-mvp/current
systemctl enable stablepay-mvp-payment.service stablepay-mvp-runtime.service
systemctl restart stablepay-mvp-payment.service stablepay-mvp-runtime.service
ready=0
for attempt in $(seq 1 30); do
    if curl -fsS --max-time 2 http://127.0.0.1:8092/healthz >/dev/null 2>&1 && curl -fsS --max-time 2 http://127.0.0.1:8093/api/mvp/status >/dev/null 2>&1; then ready=1; break; fi
    sleep 1
done
if [[ "$ready" -ne 1 ]]; then
    if [[ -n "$previous_release" && -d "$previous_release" ]]; then
        if [[ -d "$rollback_dir/config" ]]; then cp -a "$rollback_dir/config/." /etc/stablepay-mvp/; fi
        ln -sfn "$previous_release" /opt/stablepay-mvp/current
        systemctl restart stablepay-mvp-payment.service stablepay-mvp-runtime.service
    else
        systemctl stop stablepay-mvp-payment.service stablepay-mvp-runtime.service
    fi
    echo 'Native health checks failed; previous release restored where available'
    exit 1
fi
backup=/etc/caddy/Caddyfile.before-stablepay-$release_id
cp -a /etc/caddy/Caddyfile "$backup"
if [[ -f /etc/caddy/stablepay-mvp.caddy ]]; then cp -a /etc/caddy/stablepay-mvp.caddy "$rollback_dir/Caddy.fragment"; fi
sed "s/PUBLIC_HOST/$public_host/g" "$release/deploy/Caddyfile.fragment" > /etc/caddy/stablepay-mvp.caddy
if ! grep -Fqx 'import /etc/caddy/stablepay-mvp.caddy' /etc/caddy/Caddyfile; then
    printf '\nimport /etc/caddy/stablepay-mvp.caddy\n' >> /etc/caddy/Caddyfile
fi
if ! caddy validate --config /etc/caddy/Caddyfile >/dev/null 2>&1 || ! systemctl reload caddy; then
    cp -a "$backup" /etc/caddy/Caddyfile
    if [[ -f "$rollback_dir/Caddy.fragment" ]]; then cp -a "$rollback_dir/Caddy.fragment" /etc/caddy/stablepay-mvp.caddy; fi
    systemctl reload caddy
    echo 'Caddy change rejected; original configuration restored'
    exit 1
fi
echo "Installed release $release_id; public callback awaits HTTPS and external reachability checks"
systemctl show stablepay-mvp.slice -p MemoryCurrent -p MemoryHigh -p MemoryMax -p CPUQuotaPerSecUSec
curl -fsS --max-time 3 http://127.0.0.1:8093/api/mvp/status
