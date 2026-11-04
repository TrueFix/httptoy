#!/bin/sh
# Build raw, self-contained release executables from a pre-built frontend checkout.
set -eu

output=${1:?Usage: release.sh OUTPUT VERSION COMMIT}
version=${2:?Missing version tag}
commit=${3:?Missing commit}

if ! printf '%s\n' "$version" | grep -Eq '^v([2-9]|[1-9][0-9]+)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$'; then
  echo 'Expected a stable version tag starting at v2.0.0.' >&2
  exit 1
fi
case "$commit" in
  *[!a-zA-Z0-9._+-]*) echo 'Invalid commit identifier.' >&2; exit 1 ;;
esac

go_license="$(go env GOROOT)/LICENSE"
if [ ! -f "$go_license" ]; then
  # Some distro packages move the license outside GOROOT. This is the official
  # Go BSD license, originally mirrored from Go's go1.26.10 tag.
  go_license=licenses/Go-LICENSE
fi
for notice in LICENSE THIRD_PARTY_NOTICES.md FRONTEND_NOTICES.md "$go_license"; do
  if [ ! -f "$notice" ]; then
    echo "Missing license notice: $notice" >&2
    exit 1
  fi
done

mkdir -p "$output"
if [ -n "$(ls -A "$output")" ]; then
  echo 'Release output directory must be empty.' >&2
  exit 1
fi

# Keep this matrix aligned with the Linux/macOS/Windows targets in `go tool dist list`.
# Conservative CPU baselines keep the binaries useful on older hardware too.
for target in \
  linux/386 linux/amd64 linux/arm linux/arm64 linux/loong64 \
  linux/mips linux/mipsle linux/mips64 linux/mips64le \
  linux/ppc64 linux/ppc64le linux/riscv64 linux/s390x \
  darwin/amd64 darwin/arm64 \
  windows/386 windows/amd64 windows/arm64; do
  os=${target%/*}
  arch=${target#*/}
  name="httptoy_${version}_${os}_${arch}"
  if [ "$os" = windows ]; then name="$name.exe"; fi
  echo "Building $name"
  CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" GO386=sse2 GOAMD64=v1 GOARM=6 \
    GOMIPS=softfloat GOMIPS64=softfloat go build -mod=readonly -trimpath \
    -ldflags="-s -w -X github.com/TrueFix/httptoy/pkg/server.buildVersion=$version -X github.com/TrueFix/httptoy/pkg/server.buildCommit=$commit" \
    -o "$output/$name" .
done

cp LICENSE THIRD_PARTY_NOTICES.md FRONTEND_NOTICES.md "$output/"
cp "$go_license" "$output/Go-LICENSE"
(
  cd "$output"
  sha256sum httptoy_* LICENSE THIRD_PARTY_NOTICES.md FRONTEND_NOTICES.md Go-LICENSE > SHA256SUMS
)
