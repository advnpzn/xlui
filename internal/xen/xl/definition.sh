# Runs in the trusted Dom0 configuration directory. Guest data arrives on stdin.
set -eu
umask 077
fail() { printf '%s\n' "$1" >&2; exit 1; }
[ ! -f /etc/xensource-inventory ] && [ ! -f /etc/xensource/inventory ] || fail "Use a XenAPI connection for this managed host."
cd "$1"
file="./$2.cfg"
mode=$3
exec 9<.
flock -x -w 5 9 || fail "Guest definitions are busy."
[ ! -L "$file" ] || fail "Refusing a symbolic link."
case "$mode" in
read)
    [ -f "$file" ] || fail "No guest definition with that name."
    head -c 16385 "$file"
    exit ;;
check-new|create)
    [ ! -e "$file" ] || fail "A guest definition with that name already exists."
    for image in "$5" "$6"; do
        [ -f "$image" ] && [ -r "$image" ] || fail "Kernel and initramfs must be readable regular files on Dom0."
    done ;;
check-edit|edit|inspect-boot|boot)
    [ -f "$file" ] || fail "Guest definition disappeared."
    digest=$(sha256sum "$file")
    [ "${digest%% *}" = "$4" ] || fail "Guest definition changed. Review it again." ;;
*) exit 2 ;;
esac
tmp=$(mktemp ./.xlui-XXXXXXXX)
trap 'rm -f "$tmp"' EXIT
cat > "$tmp"
if [ "$mode" = inspect-boot ]; then
    xl create -n -q "$tmp" 9<&-
    exit
fi
xl create -n -q "$tmp" >/dev/null 9<&- || fail "xl rejected the proposed boot definition."
case "$mode" in
check-edit|edit)
    [ "$(stat -c %h "$file")" = 1 ] || fail "Refusing to replace a multiply-linked guest definition."
    # Standalone BusyBox ash prefers its cp applet even if GNU cp is on PATH.
    # Invoke the first external cp by pathname so both shells use the same tool.
    search=$PATH:
    while [ -n "$search" ]; do
        dir=${search%%:*}
        search=${search#*:}
        copy=${dir:-.}/cp
        if [ -f "$copy" ] && [ -x "$copy" ]; then
            "$copy" --attributes-only --preserve=all "$file" "$tmp" 2>/dev/null ||
                fail "Could not preserve definition metadata. Check file permissions and that cp on PATH supports --attributes-only --preserve=all (GNU coreutils)."
            break
        fi
        [ -n "$search" ] || fail "Install GNU coreutils cp on PATH to preserve definition metadata."
    done
    touch "$tmp"
    digest=$(sha256sum "$file")
    [ "${digest%% *}" = "$4" ] || fail "Guest definition changed during validation."
    [ "$mode" = check-edit ] || mv -T "$tmp" "$file" ;;
check-new) exit ;;
boot) xl create -q "$tmp" 9<&- >/dev/null ;;
create)
    # Link without replacement and start precisely the checked bytes. Prevent
    # xl's domain monitor from inheriting the directory lock.
    ln "$tmp" "$file" || fail "Could not publish the new definition."
    xl create -q "$tmp" 9<&- >/dev/null || fail "Definition saved; guest start was not verified."
    ;;
esac
