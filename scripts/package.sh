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

if ! printf '%s\n' "$version" | LC_ALL=C grep -Eq '^[0-9]+\.[0-9]+\.[0-9]+$'; then
  echo "VERSION must contain MAJOR.MINOR.PATCH numeric components" >&2
  exit 1
fi

temporary_directory=$(mktemp -d "${TMPDIR:-/tmp}/processpilot-package.XXXXXX")
trap 'rm -rf "$temporary_directory"' EXIT HUP INT TERM
package_name="processpilot-${version}-darwin-${archive_architecture}"
package_directory="$temporary_directory/$package_name"
output_directory="$repository_directory/dist"
archive_path="$output_directory/$package_name.tar.gz"
archive_tar_path="$temporary_directory/$package_name.tar"
source_date_epoch=${SOURCE_DATE_EPOCH:-$(git -C "$repository_directory" log -1 --format=%ct)}

case "$source_date_epoch" in
  ''|*[!0-9]*)
    echo "SOURCE_DATE_EPOCH must be a Unix timestamp" >&2
    exit 1
    ;;
esac
archive_timestamp=$(date -u -r "$source_date_epoch" +%Y%m%d%H%M.%S)

mkdir -p "$package_directory" "$output_directory"
cd "$repository_directory"
rustup target add "$rust_target"
cargo build --release --locked --target "$rust_target"
CGO_ENABLED=0 GOOS=darwin GOARCH="$go_architecture" go build -trimpath -ldflags="-s -w" -o "$package_directory/processpilot" ./cmd/processpilot
cp "$repository_directory/target/$rust_target/release/processpilot-collector" "$package_directory/processpilot-collector"
cp "$repository_directory/README.md" "$repository_directory/LICENSE" "$package_directory/"
chmod 755 "$package_directory/processpilot" "$package_directory/processpilot-collector"
touch -t "$archive_timestamp" "$package_directory" "$package_directory/processpilot" "$package_directory/processpilot-collector" "$package_directory/README.md" "$package_directory/LICENSE"

COPYFILE_DISABLE=1 tar -C "$temporary_directory" -cf "$archive_tar_path" --format ustar --uid 0 --gid 0 --uname root --gname wheel \
  "$package_name/processpilot" \
  "$package_name/processpilot-collector" \
  "$package_name/README.md" \
  "$package_name/LICENSE"
gzip -n -9 < "$archive_tar_path" > "$archive_path"
(
  cd "$output_directory"
  shasum -a 256 "$(basename "$archive_path")" > "$(basename "$archive_path").sha256"
)
printf '%s\n' "$archive_path"
