#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")"

version="${ATRACE_APP_VERSION:-0.3.${GITHUB_RUN_NUMBER:-0}}"
config_backup="$(mktemp)"
cp mygo.json "$config_backup"
trap 'cp "$config_backup" mygo.json; rm -f "$config_backup"' EXIT
python3 - "$version" <<'VERSION'
import json, os, pathlib, sys
path = pathlib.Path('mygo.json')
config = json.loads(path.read_text())
config['version'] = sys.argv[1]
url = os.environ.get('ATRACE_UPDATE_URL', '')
key = os.environ.get('ATRACE_UPDATE_PUBLIC_KEY', '')
if bool(url) != bool(key) or (os.environ.get('MYGO_UPDATER_PRIVATE_KEY') and not url):
    raise SystemExit('Set ATRACE_UPDATE_URL and ATRACE_UPDATE_PUBLIC_KEY together to enable updates')
if url:
    config['updates'] = {'url': url, 'publicKey': key, 'deltas': 0}
path.write_text(json.dumps(config, ensure_ascii=False, indent=2) + '\n')
VERSION

# MyGo cross-compiles the App using Go alone; no Xcode or DMG is required.
mkdir -p build/runtime/bin build/runtime/examples
bun_binary="${ATRACE_APP_BUN:-}"
if [[ -z "$bun_binary" ]]; then
  if [[ "$(uname -s)" == Darwin && "$(uname -m)" == arm64 ]]; then
    bun_binary="$(command -v bun)"
  else
    cache="${XDG_CACHE_HOME:-$HOME/.cache}/atrace-app/bun-1.4.2"
    mkdir -p "$cache"
    if [[ ! -f "$cache/bun" ]]; then
      curl --http1.1 --fail --silent --show-error --location --retry 2 --connect-timeout 10 --max-time 60 \
        "https://registry.npmjs.org/@oven/bun-darwin-aarch64/-/bun-darwin-aarch64-1.4.2.tgz" -o "$cache/bun.tgz"
      python3 - "$cache" <<'PYCODE'
import base64,hashlib,pathlib,sys,tarfile
p=pathlib.Path(sys.argv[1]); archive=p/'bun.tgz'
expected='MXdZkP1featqxZ+/VTXWG1BVjM4OGBehVY2Q88EeUj/7L0UMeCGItmyPYTN+wxvlGJ6F66JEtzsw+GvQWewnag=='
assert base64.b64encode(hashlib.sha512(archive.read_bytes()).digest()).decode()==expected, 'Bun checksum mismatch'
with tarfile.open(archive) as t:
    binary=t.extractfile('package/bin/bun').read()
(p/'bun').write_bytes(binary)
PYCODE
    fi
    bun_binary="$cache/bun"
  fi
fi
rm -f build/runtime/bin/bun
cp "$bun_binary" build/runtime/bin/bun
chmod +x build/runtime/bin/bun
cp ../cli/atrace build/runtime/cli.mjs
cat > build/runtime/bin/atrace <<'CLI'
#!/bin/sh
bin="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
exec "$bin/bun" "$bin/../cli.mjs" "$@"
CLI
chmod +x build/runtime/bin/atrace
# Python is also available on the Linux CI image; keep resources identical on either host.
python3 - <<'PY'
import shutil
shutil.rmtree('build/runtime/examples')
shutil.copytree('../cli/collector-examples','build/runtime/examples',ignore=shutil.ignore_patterns('.DS_Store'))
PY
(unset MYGO_UPDATER_PRIVATE_KEY; CGO_ENABLED=0 go tool mygo build -platform darwin/arm64 -skip-dmg -sign -)
app="$PWD/build/darwin-arm64/Atrace.app"
# Seal the bundle with an ad-hoc signature on either build host. Keep Bun's
# upstream signature and JIT entitlements; only sign the enclosing App here.
if [[ "$(uname -s)" == Darwin ]]; then
  codesign --verify --deep --strict "$app"
  "$app/Contents/Resources/bin/atrace" --help > /dev/null
else
  if [[ "$(uname -m)" != x86_64 ]]; then echo "Linux packaging currently requires x86_64" >&2; exit 1; fi
  signer="${XDG_CACHE_HOME:-$HOME/.cache}/atrace-app/rcodesign-0.29.0"
  mkdir -p "$signer"
  if [[ ! -x "$signer/rcodesign" ]]; then
    curl --http1.1 --fail --silent --show-error --location --retry 2 --connect-timeout 10 --max-time 60 \
      "https://github.com/indygreg/apple-platform-rs/releases/download/apple-codesign%2F0.29.0/apple-codesign-0.29.0-x86_64-unknown-linux-musl.tar.gz" -o "$signer/tool.tgz"
    python3 - "$signer" <<'PYCODE'
import hashlib,pathlib,sys,tarfile
p=pathlib.Path(sys.argv[1]);archive=p/'tool.tgz'
assert hashlib.sha256(archive.read_bytes()).hexdigest()=='dbe85cedd8ee4217b64e9a0e4c2aef92ab8bcaaa41f20bde99781ff02e600002', 'rcodesign checksum mismatch'
with tarfile.open(archive) as t:
    binary=t.extractfile('apple-codesign-0.29.0-x86_64-unknown-linux-musl/rcodesign').read()
(p/'rcodesign').write_bytes(binary)
(p/'rcodesign').chmod(0o755)
PYCODE
  fi
  "$signer/rcodesign" sign --shallow "$app"
fi
python3 - "$app" <<'PY'
import pathlib,sys,zipfile
app=pathlib.Path(sys.argv[1]); target=app.parent/'Atrace-macos-arm64.zip'
with zipfile.ZipFile(target,'w',zipfile.ZIP_DEFLATED,compresslevel=1) as z:
    for p in sorted(app.rglob('*')):
        if p.is_file(): z.write(p,p.relative_to(app.parent))
print(f'Built {target}')
PY

bun package-update.mjs "$app"
