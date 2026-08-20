#!/bin/sh
set -eu

repository_directory=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
version=$(tr -d '[:space:]' < "$repository_directory/VERSION")
target_architecture=${PROCESSPILOT_TARGET_ARCH:-$(uname -m)}

case "$target_architecture" in
  arm64|aarch64)
    archive_architecture=arm64
    rust_target=aarch64-apple-darwin
    go_architecture=arm64
    ;;
  amd64|x86_64)
    archive_architecture=amd64
    rust_target=x86_64-apple-darwin
    go_architecture=amd64
    ;;
  *)
    echo "unsupported macOS architecture: $target_architecture" >&2
    exit 1
    ;;
esac

case "$version" in
  ''|*[!0-9.]*|.*|*.)
    echo "VERSION must contain a semantic numeric version" >&2
    exit 1
    ;;
esac

temporary_directory=$(mktemp -d "${TMPDIR:-/tmp}/processpilot-package.XXXXXX")
trap 'rm -rf "$temporary_directory"' EXIT HUP INT TERM
package_name="processpilot-${version}-darwin-${archive_architecture}"
package_directory="$temporary_directory/$package_name"
output_directory="$repository_directory/dist"
archive_path="$output_directory/$package_name.tar.gz"

mkdir -p "$package_directory" "$output_directory"
cd "$repository_directory"
rustup target add "$rust_target"
cargo build --release --locked --target "$rust_target"
CGO_ENABLED=0 GOOS=darwin GOARCH="$go_architecture" go build -trimpath -ldflags="-s -w" -o "$package_directory/processpilot" ./cmd/processpilot
cp "$repository_directory/target/$rust_target/release/processpilot-collector" "$package_directory/processpilot-collector"
cp "$repository_directory/README.md" "$repository_directory/LICENSE" "$package_directory/"
chmod 755 "$package_directory/processpilot" "$package_directory/processpilot-collector"

tar -C "$temporary_directory" -czf "$archive_path" "$package_name"
shasum -a 256 "$archive_path" > "$archive_path.sha256"
printf '%s\n' "$archive_path"
