#!/usr/bin/env python3
"""
genconfig.py — 把 config/rules/*.json 目录渲染为 Caddyfile 与 hosts 片段。

用法:
    python3 tools/genconfig.py                    # 渲染启用规则 -> Caddyfile + S302.hosts
    python3 tools/genconfig.py --all              # 渲染全部规则（含默认关闭）
    python3 tools/genconfig.py --caddyfile out.txt
    python3 tools/genconfig.py --hosts out.hosts
    python3 tools/genconfig.py --no-validate      # 跳过 caddy adapt 校验
    python3 tools/genconfig.py --print            # 直接打印 Caddyfile 到 stdout

P0 阶段等效逻辑将迁入 Go（internal/rules + cmd/genconfig）。
"""

import argparse
import json
import os
import subprocess
import sys

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
RULES_DIR = os.path.join(ROOT, "config", "rules")
ENV_FILE = os.path.join(ROOT, "config", "env.json")


def load_json(path):
    with open(path, encoding="utf-8") as f:
        return json.load(f)


def load_env():
    return load_json(ENV_FILE)


def load_rules(include_disabled=False):
    rules = []
    for fn in sorted(os.listdir(RULES_DIR)):
        if not fn.endswith(".json"):
            continue
        rule = load_json(os.path.join(RULES_DIR, fn))
        rid = rule.get("id") or fn[:-5]
        rule["id"] = rid
        if not rule.get("enabled") and not include_disabled:
            continue
        rules.append(rule)
    return rules


def caddy_quote(s):
    s = s.replace("\\", "\\\\").replace('"', '\\"')
    if " " in s or not s:
        return '"' + s + '"'
    return s


def render_match(idx, match):
    """返回 (matcher_name_or_None, block_text)"""
    if not match:
        return None, ""
    name = f"m{idx}"
    lines = []
    for p in match.get("path", []):
        lines.append(f"    path {p}")
    for m in match.get("method", []):
        lines.append(f"    method {m}")
    for ex in match.get("expression", []):
        lines.append(f"    expression {ex}")
    for notm in match.get("not", []):
        for p in notm.get("path", []):
            lines.append(f"    not path {p}")
        for m in notm.get("method", []):
            lines.append(f"    not method {m}")
        for ex in notm.get("expression", []):
            lines.append(f"    not expression {ex}")
    block = f"@{name} {{\n" + "\n".join(lines) + "\n}"
    return name, block


def render_header_up(entries):
    lines = []
    for e in entries or []:
        if len(e) == 2:
            name, val = e
            if val is None:
                lines.append(f"    header_up -{name}")
            else:
                lines.append(f"    header_up {name} {caddy_quote(val)}")
        elif len(e) == 3:  # 正则替换: [name, match, value]
            name, pat, val = e
            lines.append(f"    header_up {name} {caddy_quote(str(pat))} {caddy_quote(str(val))}")
    return "\n".join(lines)


def render_header_down(entries):
    lines = []
    for e in entries or []:
        if not e:
            continue
        name = e[0]
        if len(e) == 1 or e[1] is None:
            lines.append(f"    header_down -{name}")
        else:
            val = e[-1]
            lines.append(f"    header_down {name} {caddy_quote(val)}")
    return "\n".join(lines)


def render_transport(t):
    if not t:
        return ""
    lines = ["    transport http {"]
    if t.get("tls"):
        lines.append("        tls")
    if t.get("tls_server_name"):
        lines.append(f"        tls_server_name {caddy_quote(t['tls_server_name'])}")
    if t.get("insecure_skip_verify"):
        lines.append("        tls_insecure_skip_verify")
    lines.append("    }")
    return "\n".join(lines)


def render_reverse_proxy(prefix, matcher, h, env):
    out = []
    out.append(f"{prefix}reverse_proxy {matcher}".strip())
    ups = h.get("upstreams") or []
    for u in ups:
        out[-1] += " " + (u if u.startswith(("https://", "http://")) else "https://" + u)
    if not ups and not h.get("dynamic_upstreams"):
        return ""  # 无上游则不渲染该 handler
    out[-1] += " {"
    block = []
    for d in h.get("dynamic_upstreams") or []:
        block.append(f"    dynamic {d.get('dns', 'a')} {d['host']} {d.get('port', 443)}")
    lb = h.get("lb") or {}
    pol = lb.get("policy")
    if pol:
        line = f"    lb_policy {pol}"
        if pol == "random_choose" and lb.get("choose"):
            line += f" {lb['choose']}"
        block.append(line)
    if lb.get("try_duration"):
        block.append(f"    lb_try_duration {lb['try_duration']}")
    if lb.get("fail_duration"):
        block.append(f"    fail_duration {lb['fail_duration']}")
    if lb.get("max_fails"):
        block.append(f"    max_fails {lb['max_fails']}")
    if lb.get("unhealthy_latency"):
        block.append(f"    unhealthy_latency {lb['unhealthy_latency']}")
    if lb.get("unhealthy_status"):
        block.append(f"    unhealthy_status {lb['unhealthy_status']}")
    hup = render_header_up(h.get("header_up"))
    if hup:
        block.append(hup)
    hdown = render_header_down(h.get("header_down"))
    if hdown:
        block.append(hdown)
    tr = render_transport(h.get("transport"))
    if tr:
        block.append(tr)
    block.append("}")
    out.append("\n".join(block))
    return "\n".join(out)


def render_handler(prefix, h, idx, env):
    name, mblock = render_match(idx, h.get("match"))
    matcher = f"@{name} " if name else ""
    seg = []
    if mblock:
        seg.append(mblock)
    t = h["type"]
    if t == "reverse_proxy":
        seg.append(render_reverse_proxy(prefix, matcher.strip(), h, env))
    elif t == "file_server":
        fs = h.get("file_server") or {}
        root = fs.get("root", ".")
        parts = [f"{prefix}file_server {matcher}".strip() + " {"]
        parts.append(f"{prefix}    root {root}")
        if fs.get("browse"):
            parts.append(f"{prefix}    browse")
        parts.append(f"{prefix}}}")
        seg.append("\n".join(parts))
    elif t == "respond":
        r = h.get("respond") or {}
        status = r.get("status", 200)
        body = r.get("body", "")
        line = f"{prefix}respond {matcher}".strip() + f" {status}"
        if body:
            line += " {\n" + prefix + "    body " + caddy_quote(body) + "\n" + prefix + "}"
        elif r.get("close"):
            line += " {\n" + prefix + "    close\n" + prefix + "}"
        seg.append(line)
    elif t == "redir":
        r = h.get("redir") or {}
        suffix = "permanent" if r.get("permanent") else ""
        seg.append((f"{prefix}redir {matcher}".strip() + f" {r.get('to', '{uri}')} {suffix}").rstrip())
    elif t == "rewrite":
        rew = h.get("rewrite") or {}
        seg.append((f"{prefix}rewrite {matcher}".strip() + f" {rew.get('to', '')}").rstrip())
    return "\n".join(x for x in seg if x)


def render_cors(prefix):
    return (
        f"{prefix}@preflight {{\n"
        f"{prefix}    method OPTIONS\n"
        f"{prefix}}}\n"
        f"{prefix}respond @preflight 204\n"
        f"{prefix}header {{\n"
        f"{prefix}    defer\n"
        f'{prefix}    Access-Control-Allow-Origin {{http.request.header.Origin}}\n'
        f"{prefix}    Access-Control-Allow-Credentials true\n"
        f"{prefix}    Access-Control-Allow-Methods *\n"
        f"{prefix}    Access-Control-Allow-Headers *\n"
        f"{prefix}    Access-Control-Allow-Private-Network true\n"
        f"{prefix}    Access-Control-Request-Private-Network true\n"
        f"{prefix}}}\n"
    )


def render_extra_headers(prefix, headers):
    if not headers:
        return ""
    lines = [f"{prefix}header {{"]
    for hh in headers:
        if hh.get("defer"):
            lines.append(f"{prefix}    defer")
        lines.append(f"{prefix}    {hh['name']} {caddy_quote(hh['value'])}")
    lines.append(f"{prefix}}}")
    return "\n".join(lines)


def render_site(site, env):
    port = env["listen"]["https_port"]
    hosts = " ".join(f"https://{h}:{port}" for h in site["hosts"])
    cert = site.get("tls_cert") or env["cert"]["cert_file"]
    key = site.get("tls_key") or env["cert"]["key_file"]
    lines = [f"{hosts} {{", f"    tls {cert} {key}"]
    if site.get("pna_cors"):
        lines.append(render_cors("    "))
    extra = render_extra_headers("    ", site.get("extra_headers"))
    if extra:
        lines.append(extra)
    for i, h in enumerate(site.get("handlers", [])):
        r = render_handler("    ", h, i, env)
        if r:
            lines.append(r)
    lines.append("}")
    return "\n".join(lines)


def render_global(env):
    l = env["listen"]
    aut = env["listen"].get("auto_https", "off")
    lines = ["{"]
    if l.get("admin_off"):
        lines.append("    admin off")
    lines.append(f"    auto_https {aut}")
    bind = env["listen"].get("bind_ip", "")
    if bind and bind != "0.0.0.0":
        lines.append(f"    default_bind {bind}")
    lines.append("}")
    return "\n".join(lines)


def render_http_redirect(env):
    port = env["listen"].get("http_port", 0)
    if not port:
        return ""
    return f"http://:{port} {{\n    redir https://{{host}}{{uri}} permanent\n}}"


def generate_caddyfile(env, rules):
    parts = [render_global(env), ""]
    for rule in rules:
        for site in rule["sites"]:
            parts.append(render_site(site, env))
            parts.append("")
    http = render_http_redirect(env)
    if http:
        parts.append(http)
        parts.append("")
    return "\n".join(parts).rstrip() + "\n"


def generate_hosts(env, rules, include_disabled=None):
    marker = env["hosts"]["marker"]
    seen = set()
    lines = []
    for rule in rules:
        for site in rule["sites"]:
            for h in site["hosts"]:
                if h in seen:
                    continue
                seen.add(h)
                lines.append(f"127.0.0.1 {h} {marker}")
    return "\n".join(sorted(lines)) + "\n"


def check_needs_files(rule):
    missing = []
    for p in rule.get("needs_files") or []:
        if not os.path.exists(os.path.join(ROOT, p)):
            missing.append(p)
    return missing


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--all", action="store_true", help="渲染全部规则（含默认关闭）")
    ap.add_argument("--caddyfile", default=os.path.join(ROOT, "Caddyfile"))
    ap.add_argument("--hosts", default=None, help="输出 hosts 片段路径")
    ap.add_argument("--print", action="store_true")
    ap.add_argument("--no-validate", action="store_true")
    args = ap.parse_args()

    env = load_env()
    rules = load_rules(include_disabled=args.all)
    if not rules:
        print("没有可用规则（全部为 enabled=false？用 --all）", file=sys.stderr)
        sys.exit(2)

    problems = []
    for r in rules:
        for p in check_needs_files(r):
            problems.append(f"[{r['id']}] 缺少依赖文件: {p}（该服务将无法渲染，先创建或改 enabled=false）")
    if problems:
        for p in problems:
            print(p, file=sys.stderr)

    cf = generate_caddyfile(env, rules)
    if args.print:
        print(cf)
        return

    with open(args.caddyfile, "w", encoding="utf-8") as f:
        f.write(cf)
    print(f"Caddyfile 已生成: {args.caddyfile}  (启用 {len(rules)} 条规则)")

    if args.hosts:
        with open(args.hosts, "w", encoding="utf-8") as f:
            f.write(generate_hosts(env, rules))
        print(f"hosts 片段已生成: {args.hosts}")

    if not args.no_validate:
        r = subprocess.run(
            ["caddy", "adapt", "--config", args.caddyfile, "--validate"],
            capture_output=True, text=True, timeout=60,
        )
        if r.returncode != 0:
            print("caddy adapt 校验失败:", file=sys.stderr)
            print(r.stderr or r.stdout, file=sys.stderr)
            sys.exit(1)
        print("caddy adapt --validate: OK")


if __name__ == "__main__":
    main()