#!/bin/sh
# Builds dist/parent-tracker_<version>_<arch>.deb from already built binaries.
#
#   packaging/build-deb.sh <version> <arch> [bin-dir]
#
#   version   as from `git describe`; made Debian-valid here
#   arch      amd64 or arm64
#   bin-dir   directory with parent-trackerd, parent-tracker, parent-tracker-agent
#             (default: bin)
#
# Needs only dpkg-deb. Run from anywhere; `make deb` builds the binaries first.
# DIST names the output directory (default: dist).
set -eu
cd "$(dirname "$0")/.."

raw=${1:?usage: build-deb.sh <version> <arch> [bin-dir]}
arch=${2:?usage: build-deb.sh <version> <arch> [bin-dir]}
bin=${3:-bin}
case "$arch" in amd64|arm64) ;; *) echo "unsupported architecture: $arch (amd64 or arm64)" >&2; exit 2 ;; esac
for b in parent-trackerd parent-tracker parent-tracker-agent; do
  [ -x "$bin/$b" ] || { echo "missing $bin/$b: run make build" >&2; exit 1; }
done

# A Debian version starts with a digit and may not contain '-' except before the
# package revision. git describe output becomes e.g. 0.1.0+3.gabc1234 or
# 0.0.0~git.abc1234 when there is no tag.
version=$(printf '%s' "$raw" | sed -E \
  -e 's/^v//' \
  -e 's/-([0-9]+)-g([0-9a-f]+)/+\1.g\2/' \
  -e 's/-dirty$/+dirty/' \
  -e 's/-dev$/~dev/')
# No tag: `git describe --always` prints a bare commit hash (optionally +dirty),
# which would sort unpredictably against real versions. Use 0.0.0~git<count>.<hash>
# so builds still sort by age and any tagged release is newer.
case "$version" in
  *[!0-9a-f+dirty]*) hash= ;;
  *) hash=$(printf '%s' "$version" | sed -E 's/\+dirty$//') ;;
esac
if printf '%s' "$hash" | grep -qE '^[0-9a-f]{7,40}$'; then
  count=$(git rev-list --count HEAD 2>/dev/null || echo 0)
  suffix=$(printf '%s' "$version" | sed -n 's/^[0-9a-f]*\(+dirty\)$/\1/p')
  version="0.0.0~git$count.$hash$suffix"
fi
case "$version" in
  [0-9]*) ;;
  *) version="0.0.0~git.$version" ;;
esac
case "$version" in *-*) echo "version $version still has a hyphen" >&2; exit 1 ;; esac

maintainer=${MAINTAINER:-"samiferr <samiferr@users.noreply.github.com>"}
out=${DIST:-dist}
pkg=parent-tracker
root=$(mktemp -d)
chmod 0755 "$root"
trap 'rm -rf "$root"' EXIT
uuid=parent-tracker@samiferr.github.io

inst() { # mode source dest
  install -D -m "$1" "$2" "$root/$3"
}

inst 0755 "$bin/parent-trackerd"      usr/bin/parent-trackerd
inst 0755 "$bin/parent-tracker"       usr/bin/parent-tracker
inst 0755 "$bin/parent-tracker-agent" usr/bin/parent-tracker-agent

inst 0644 packaging/parent-trackerd.service      usr/lib/systemd/system/parent-trackerd.service
inst 0644 packaging/parent-tracker-agent.service usr/lib/systemd/user/parent-tracker-agent.service
inst 0644 packaging/tmpfiles.conf                usr/lib/tmpfiles.d/parent-tracker.conf
inst 0755 packaging/gnome-extension-setup        usr/lib/parent-tracker/gnome-extension-setup
inst 0755 packaging/agent-session-start          usr/lib/parent-tracker/agent-session-start

inst 0644 gnome-extension/metadata.json usr/share/gnome-shell/extensions/$uuid/metadata.json
inst 0644 gnome-extension/extension.js  usr/share/gnome-shell/extensions/$uuid/extension.js

# Starts the agent at login on every desktop. The systemd user unit is wanted by
# graphical-session.target, which Xfce (Xubuntu) never reaches.
inst 0644 packaging/parent-tracker-agent.desktop etc/xdg/autostart/parent-tracker-agent.desktop

# The notice every monitored user sees at login, and a menu entry that shows
# them what is recorded about them (SEC-2).
inst 0644 packaging/parent-tracker-notice.desktop etc/xdg/autostart/parent-tracker-notice.desktop
inst 0644 packaging/parent-tracker-self.desktop   usr/share/applications/parent-tracker-self.desktop

inst 0644 packaging/config.toml.example usr/share/doc/$pkg/config.toml.example
inst 0644 internal/server/web/notices.txt usr/share/doc/$pkg/copyright
inst 0644 README.md                      usr/share/doc/$pkg/README.md
inst 0644 docs/USER-GUIDE.md             usr/share/doc/$pkg/USER-GUIDE.md

# Shell completion comes from the CLI itself (a build for the host architecture).
mkdir -p "$root/usr/share/bash-completion/completions"
env -u GOARCH -u GOOS CGO_ENABLED=0 go run ./cmd/parent-tracker completion bash > "$root/usr/share/bash-completion/completions/parent-tracker"
chmod 0644 "$root/usr/share/bash-completion/completions/parent-tracker"

mkdir -p "$root/DEBIAN"
inst 0755 packaging/debian/postinst DEBIAN/postinst
inst 0755 packaging/debian/prerm    DEBIAN/prerm
inst 0755 packaging/debian/postrm   DEBIAN/postrm

size=$(du -sk "$root" | cut -f1)
cat > "$root/DEBIAN/control" <<CTL
Package: $pkg
Version: $version
Architecture: $arch
Maintainer: $maintainer
Installed-Size: $size
Depends: systemd (>= 255), dbus
Recommends: libnotify-bin, dconf-cli
Suggests: gnome-shell (>= 46)
Section: admin
Priority: optional
Homepage: https://github.com/samiferr/parent_tracker_ubuntu
Description: transparent screen-time tracker for a family PC
 Parent Tracker records when a child's account is signed in, active, idle or
 locked, and which application is in front, by the hour. A parent reads it on a
 local dashboard; the child can see everything recorded about them with
 "parent-tracker self". Nothing leaves the PC, and nothing is recorded until
 the administrator has acknowledged the disclosure notice.
 .
 Supports Ubuntu 24.04 and newer, on X11 and Wayland (GNOME).
CTL

mkdir -p "$out"
file="$out/${pkg}_${version}_${arch}.deb"
dpkg-deb --root-owner-group -Zxz --build "$root" "$file" >/dev/null
echo "$file"
