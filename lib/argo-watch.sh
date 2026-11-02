#!/usr/bin/env bash
set -Eeuo pipefail
ROOT="$1"; APP="$2"
for ((i=0;i<90;i++));do
    domain=$(python3 - "$ROOT/argo.log" <<'PY'
import pathlib,re,sys
p=pathlib.Path(sys.argv[1]);text=p.read_text(errors='replace') if p.exists() else ''
matches=re.findall(r'https://([a-z0-9-]+\.trycloudflare\.com)',text)
print(matches[-1] if matches else '')
PY
)
    if [[ -n "$domain" ]];then
        python3 "$APP/lib/config.py" --root "$ROOT" argo "$domain"
        exit 0
    fi
    sleep 1
done
echo '临时 Argo 域名未生成，请查看 sb-argo 日志' >&2;exit 1
