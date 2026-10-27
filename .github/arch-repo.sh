#!/bin/bash
# Builds the pacman repository for corr: one folder per architecture, each
# with the packages and the correspondent database pacman reads. Runs on
# Arch Linux (repo-add comes with pacman).
#
#   arch-repo.sh PACKAGES_DIR OUT_DIR
#
# Readers add to /etc/pacman.conf:
#   [correspondent]
#   SigLevel = Optional TrustAll
#   Server = https://arenzana.github.io/correspondent/arch/$arch
set -euo pipefail
src=$1 out=$2
shopt -s nullglob
pkgs=("$src"/*.pkg.tar.zst)
(( ${#pkgs[@]} )) || { echo "no packages in $src" >&2; exit 1; }
for pkg in "${pkgs[@]}"; do
  arch=$(basename "$pkg" .pkg.tar.zst)
  arch=${arch##*-}
  mkdir -p "$out/arch/$arch"
  cp "$pkg" "$out/arch/$arch/"
  repo-add -q "$out/arch/$arch/correspondent.db.tar.gz" "$out/arch/$arch/$(basename "$pkg")"
done
# Static hosts don't keep symlinks: make the short names real files.
for link in "$out"/arch/*/correspondent.{db,files}; do
  cp --remove-destination "$(readlink -f "$link")" "$link"
done
cat > "$out/index.html" <<'HTML'
<!doctype html>
<meta charset="utf-8">
<title>corr for Arch Linux</title>
<p>The pacman repository for <a href="https://github.com/arenzana/correspondent">corr</a>,
Correspondent's terminal reader. Add to <code>/etc/pacman.conf</code>:</p>
<pre>[correspondent]
SigLevel = Optional TrustAll
Server = https://arenzana.github.io/correspondent/arch/$arch</pre>
<p>then <code>sudo pacman -Sy corr</code>.</p>
HTML
ls -R "$out"
