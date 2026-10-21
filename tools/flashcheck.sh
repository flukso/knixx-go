#!/bin/sh
# flashcheck.sh VERSION checks, after tinygo flash reported an error, whether
# the board came back running the firmware just flashed: VERSION is the line
# its version command prints for that build, "version <commit> built <date>".
#
# The UF2 bootloader resets the board as soon as it has the last block of
# the file, and the USB drive goes away while the host still writes the
# file's FAT and directory entries. macOS returns the error of those writes
# to tinygo flash, which then reports a flash that worked as failed.
#
# It waits up to 20 s for the board's serial port and asks for the version
# a few times, as the console starts a moment after the port appears.
want=$1
tmp=$(mktemp)
trap 'rm -f "$tmp"' EXIT

case $(uname) in
Darwin) ports='/dev/cu.usbmodem*' stty_dev=-f ;;
*) ports='/dev/ttyACM*' stty_dev=-F ;;
esac

got=
i=0
while [ $i -lt 20 ]; do
	i=$((i + 1))
	sleep 1
	port=$(ls $ports 2>/dev/null | head -n 1)
	[ -n "$port" ] || continue
	stty $stty_dev "$port" 115200 raw -echo 2>/dev/null || continue
	exec 3<>"$port" || continue
	cat <&3 >"$tmp" &
	reader=$!
	sleep 0.5
	printf '\r' >&3
	sleep 0.3
	printf 'version\r' >&3
	sleep 1
	kill $reader 2>/dev/null
	wait $reader 2>/dev/null
	exec 3>&-
	got=$(tr -d '\033' <"$tmp" | tr '\r' '\n' | grep '^version ' | tail -n 1)
	[ -n "$got" ] && break
done

if [ "$got" = "$want" ]; then
	echo "flash: the copy reported an error, but the board runs the new firmware: $got"
	exit 0
fi
echo "flash: failed; the board reports \"${got:-no version}\", want \"$want\"" >&2
exit 1
