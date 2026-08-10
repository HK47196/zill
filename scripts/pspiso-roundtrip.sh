#!/bin/sh
set -eu

if [ "$#" -lt 1 ] || [ "$#" -gt 2 ]; then
	echo "usage: scripts/pspiso-roundtrip.sh SOURCE_ISO [build/iso-roundtrip/NAME]" >&2
	exit 2
fi

source_iso=$1
work_dir=${2:-build/iso-roundtrip/maintainer-proof}

if [ "$(dirname -- "$work_dir")" != "build/iso-roundtrip" ]; then
	echo "work directory must be a direct child of build/iso-roundtrip/" >&2
	exit 2
fi

if [ -e "$work_dir" ]; then
	echo "work directory already exists: $work_dir" >&2
	exit 1
fi

source_size=$(stat -c %s -- "$source_iso")
available=$(df -B1 --output=avail . | tail -n 1 | tr -d ' ')
required=$((source_size * 2 + 67108864))
if [ "$available" -lt "$required" ]; then
	echo "insufficient disk space: available=$available required=$required" >&2
	exit 1
fi

source_before=$(stat -c '%i:%s:%Y' -- "$source_iso")
roundtrip_gocache=${GOCACHE:-$PWD/build/iso-roundtrip/.gocache}
mkdir -p -- "$roundtrip_gocache"
GOCACHE=$roundtrip_gocache go run ./cmd/pspiso-roundtrip --source "$source_iso" --work "$work_dir"
source_after=$(stat -c '%i:%s:%Y' -- "$source_iso")
if [ "$source_before" != "$source_after" ]; then
	echo "source identity, size, or timestamp changed" >&2
	exit 1
fi

sha256sum -- "$source_iso" "$work_dir/rebuilt.iso"
cmp -- "$source_iso" "$work_dir/rebuilt.iso"
echo "byte-identical PSP ISO round trip proven"
