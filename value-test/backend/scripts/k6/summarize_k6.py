#!/usr/bin/env python3
import json
import pathlib
import sys
import re
from collections import defaultdict

def metric(data, name, field, default="n/a"):
    try:
        return data["metrics"][name][field]
    except Exception:
        return default

def fmt_ms(value):
    if value == "n/a":
        return value
    return f"{float(value):.2f} ms"

def fmt_rate_percent(value):
    if value == "n/a":
        return value
    return f"{float(value) * 100:.2f}%"

def fmt_rps(value):
    if value == "n/a":
        return value
    return f"{float(value):.2f} req/s"

def load(path):
    with path.open("r", encoding="utf-8") as f:
        return json.load(f)

def main():
    if len(sys.argv) < 2:
        print("usage: summarize_k6.py <report_dir> [rate1 rate2 ...]", file=sys.stderr)
        return 1

    report_dir = pathlib.Path(sys.argv[1])
    # 如果指定了速率列表则使用，否则尝试从文件名自动提取
    if len(sys.argv) > 2:
        rates = sys.argv[2:]
    else:
        # 自动扫描所有 summary 文件，提取速率
        rates = set()
        for f in report_dir.glob("*-summary-rate*.json"):
            match = re.search(r'-rate(\d+)\.json$', f.name)
            if match:
                rates.add(match.group(1))
        rates = sorted(rates, key=int)

    if not rates:
        print("No rate files found.", file=sys.stderr)
        return 1

    # 收集数据: route -> rate -> dict of metrics
    data_store = defaultdict(dict)
    # 同时记录每个路由的预期状态码（从第一个文件读取）
    route_expected = {}

    for f in report_dir.glob("*-summary-rate*.json"):
        # 解析文件名，如 /healthz 变成 _healthz-summary-rate20.json
        name_parts = f.stem.split('-summary-rate')
        if len(name_parts) != 2:
            continue
        route_part = name_parts[0].replace('_', '/')  # 还原路由
        # 如果路由以 / 开头则直接使用，否则补充 /
        if not route_part.startswith('/'):
            route_part = '/' + route_part
        rate = name_parts[1]
        if rate not in rates:
            continue
        data = load(f)
        # 提取指标
        rps = fmt_rps(metric(data, "http_reqs", "rate"))
        p95 = fmt_ms(metric(data, "http_req_duration", "p(95)"))
        p99 = fmt_ms(metric(data, "http_req_duration", "p(99)"))
        err = fmt_rate_percent(metric(data, "http_req_failed", "value"))
        # 提取预期状态码（从 tags 或直接读取，这里我们为了简单，可以从文件名或通过已知路由映射？）
        # 但我们没有存预期状态码，我们可以从第一个文件中获取 tags 里的 expected_status
        expected = metric(data, "http_reqs", "expected_status", default="?")
        if expected == "?":
            # 尝试从 tags 获取
            expected = data.get("metrics", {}).get("http_reqs", {}).get("tags", {}).get("expected_status", "?")
        # 保存
        data_store[route_part][rate] = {
            "p95": p95,
            "p99": p99,
            "error_rate": err,
            "rps": rps,
            "expected": expected
        }
        route_expected[route_part] = expected  # 覆盖

    if not data_store:
        print("No valid data found.", file=sys.stderr)
        return 1

    # 输出 Markdown 矩阵表格
    print("# Task 02 Baseline Matrix Summary\n")
    print(f"**Rates tested:** {' RPS, '.join(rates)} RPS\n")

    # 定义要展示的指标列表（每个指标一行）
    metrics_to_show = [
        ("p95", "p95 latency"),
        ("p99", "p99 latency"),
        ("error_rate", "Error Rate"),
        ("rps", "Actual RPS")
    ]

    # 按路由排序
    sorted_routes = sorted(data_store.keys())

    for metric_key, metric_label in metrics_to_show:
        print(f"### {metric_label}\n")
        # 表头
        header = "| Route | Expected Status | " + " | ".join(f"{r} RPS" for r in rates) + " |"
        sep = "| --- | --- | " + " | ".join("---" for _ in rates) + " |"
        print(header)
        print(sep)
        for route in sorted_routes:
            route_data = data_store[route]
            expected = route_expected.get(route, "?")
            row = [route, expected]
            for rate in rates:
                if rate in route_data:
                    val = route_data[rate].get(metric_key, "n/a")
                else:
                    val = "missing"
                row.append(val)
            print("| " + " | ".join(row) + " |")
        print("\n")

    # 额外输出一个汇总表格：只显示 p95 和错误率（紧凑）
    print("### Compact Summary (p95 / Error Rate)\n")
    header = "| Route | Expected | " + " | ".join(f"{r} RPS (p95/err)" for r in rates) + " |"
    sep = "| --- | --- | " + " | ".join("---" for _ in rates) + " |"
    print(header)
    print(sep)
    for route in sorted_routes:
        expected = route_expected.get(route, "?")
        row = [route, expected]
        for rate in rates:
            if rate in data_store[route]:
                p95 = data_store[route][rate].get("p95", "n/a")
                err = data_store[route][rate].get("error_rate", "n/a")
                row.append(f"{p95} / {err}")
            else:
                row.append("missing")
        print("| " + " | ".join(row) + " |")

    return 0

if __name__ == "__main__":
    raise SystemExit(main())