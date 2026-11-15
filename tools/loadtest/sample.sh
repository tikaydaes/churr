#!/bin/sh
# Records what a Churr server and its PostgreSQL use while a load test runs.
# Run it on the server's machine (Linux; it reads /proc and needs nothing
# installed):
#
#   tools/loadtest/sample.sh -i eth0 -d /var/lib/churr/data -b "$CHURR_DB" server.csv
#
#   -i IFACE    network interface to count bytes on (default: the one the
#               default route uses)
#   -d DIR      the server's data directory, measured at the end
#   -b URL      the PostgreSQL address, to ask the database its size at the
#               end (needs psql; without it, or for SQLite, the size is that
#               of the database files in -d)
#   -p PID      the churr process (default: the oldest process named churr)
#   -s SECONDS  between samples (default 2)
#   -n SECONDS  stop by itself after this long (default: run until Ctrl-C)
#
# It writes one line every two seconds until interrupted, then the sizes as
# comment lines at the end. The columns:
#
#   time              seconds since 1970
#   churr_cpu_s       CPU seconds the churr process has used since it started
#   postgres_cpu_s    the same for every postgres process together, counting
#                     those that have ended
#   churr_rss_kb      memory the churr process has resident
#   postgres_rss_kb   the same summed over the postgres processes. Each one
#                     counts the shared buffers it has touched, so this
#                     overstates; postgres_pss_kb does not
#   postgres_pss_kb   memory of the postgres processes with shared pages
#                     divided among their sharers; empty unless run as root
#                     or as the postgres user
#   idle_cpu_s        seconds the machine's CPUs have been idle since boot,
#                     all CPUs added together
#   cpus              how many CPUs that is over
#   rx_bytes tx_bytes bytes in and out on the interface since boot
#
# The CPU and byte columns only ever grow: what was used between two lines
# is the difference between them. A CPU's worth of work is one second a
# second.
set -u

# Where /proc and /sys are; only a test of this script sets them.
PROC=${SAMPLE_PROC:-/proc}
SYS=${SAMPLE_SYS:-/sys}

IFACE= DATA= DBURL= PID= EVERY=2 LIMIT=
while getopts i:d:b:p:s:n: opt; do
	case $opt in
	i) IFACE=$OPTARG ;;
	d) DATA=$OPTARG ;;
	b) DBURL=$OPTARG ;;
	p) PID=$OPTARG ;;
	s) EVERY=$OPTARG ;;
	n) LIMIT=$OPTARG ;;
	*) echo "usage: sample.sh [-i iface] [-d datadir] [-b dburl] [-p pid] [-s seconds] [-n seconds] out.csv" >&2; exit 2 ;;
	esac
done
shift $((OPTIND - 1))
OUT=${1:-}
if [ -z "$OUT" ]; then
	echo "sample.sh: name the file to write, such as server.csv" >&2
	exit 2
fi
if [ ! -r "$PROC/stat" ]; then
	echo "sample.sh: no /proc here; this runs on Linux, on the server's machine" >&2
	exit 1
fi

TICK=$(getconf CLK_TCK 2>/dev/null || echo 100)
PAGE_KB=$(($(getconf PAGESIZE 2>/dev/null || echo 4096) / 1024))

if [ -z "$IFACE" ]; then
	# The interface of the default route: the first line of /proc/net/route
	# whose destination is 00000000.
	while read -r name dest _; do
		if [ "$dest" = 00000000 ]; then
			IFACE=$name
			break
		fi
	done <"$PROC/net/route"
fi
if [ -z "$IFACE" ] || [ ! -r "$SYS/class/net/$IFACE/statistics/rx_bytes" ]; then
	echo "sample.sh: no network interface '${IFACE}'; name one with -i (see: ls /sys/class/net)" >&2
	exit 1
fi

# named NAME prints the ids of the processes with that name.
named() {
	for comm in "$PROC"/[0-9]*/comm; do
		if read -r name 2>/dev/null <"$comm" && [ "$name" = "$1" ]; then
			pid=${comm#"$PROC"/}
			echo "${pid%/comm}"
		fi
	done
}

if [ -z "$PID" ]; then
	# The oldest: the lowest start time, field 22 of stat.
	best=
	for p in $(named churr); do
		read -r stat 2>/dev/null <"$PROC/$p/stat" || continue
		set -- ${stat##*) }
		if [ -z "$best" ] || [ "${20}" -lt "$best" ]; then
			best=${20}
			PID=$p
		fi
	done
fi
if [ -z "$PID" ] || [ ! -r "$PROC/$PID/stat" ]; then
	echo "sample.sh: no churr process found; give its id with -p" >&2
	exit 1
fi

# ticks PID WITH_CHILDREN prints the CPU ticks a process has used: user and
# system, and with the second argument 1 those of its children that have
# ended, which is where a finished postgres backend's time goes. The fields
# are counted from after the name, which may itself hold spaces.
ticks() {
	read -r stat 2>/dev/null <"$PROC/$1/stat" || { echo 0; return; }
	set -- "$2" ${stat##*) }
	# $2 is field 3 of stat (the state), so field 14 is $13.
	if [ "$1" = 1 ]; then
		echo $((${13} + ${14} + ${15} + ${16}))
	else
		echo $((${13} + ${14}))
	fi
}

# rss_kb PID prints a process's resident memory.
rss_kb() {
	if read -r _ pages _ 2>/dev/null <"$PROC/$1/statm"; then
		echo $((pages * PAGE_KB))
	else
		echo 0
	fi
}

# pss_kb PID prints its proportional share, or nothing when that may not be
# read.
pss_kb() {
	[ -r "$PROC/$1/smaps_rollup" ] || return 0
	while read -r key value _; do
		if [ "$key" = Pss: ]; then
			echo "$value"
			return 0
		fi
	done 2>/dev/null <"$PROC/$1/smaps_rollup"
}

# seconds TICKS prints ticks as seconds with two decimals.
seconds() {
	echo "$(($1 / TICK)).$(printf %02d $(($1 % TICK * 100 / TICK)))"
}

finish() {
	trap - INT TERM
	if [ -n "$DATA" ]; then
		echo "# data_dir_kb,$(du -sk "$DATA" 2>/dev/null | cut -f1)" >>"$OUT"
	fi
	size=
	if [ -n "$DBURL" ] && command -v psql >/dev/null 2>&1; then
		size=$(psql "$DBURL" -Atc 'select pg_database_size(current_database())' 2>/dev/null)
		[ -n "$size" ] && size=$((size / 1024))
	fi
	if [ -z "$size" ] && [ -n "$DATA" ] && [ -f "$DATA/churr.db" ]; then
		# SQLite: the database and what it has not yet folded into it.
		size=$(du -ck "$DATA"/churr.db* 2>/dev/null | tail -1 | cut -f1)
	fi
	echo "# database_kb,${size:-unknown}" >>"$OUT"
	echo "sample.sh: wrote $OUT" >&2
	exit 0
}
trap finish INT TERM

echo "time,churr_cpu_s,postgres_cpu_s,churr_rss_kb,postgres_rss_kb,postgres_pss_kb,idle_cpu_s,cpus,rx_bytes,tx_bytes" >"$OUT"
echo "sample.sh: churr is process $PID, network is $IFACE; a line every ${EVERY}s to $OUT; Ctrl-C to finish" >&2

began=$(date +%s)
while :; do
	now=$(date +%s)
	churr_ticks=$(ticks "$PID" 0)
	churr_rss=$(rss_kb "$PID")

	pg_ticks=0 pg_rss=0 pg_pss=
	for p in $(named postgres); do
		pg_ticks=$((pg_ticks + $(ticks "$p" 1)))
		pg_rss=$((pg_rss + $(rss_kb "$p")))
		share=$(pss_kb "$p")
		[ -n "$share" ] && pg_pss=$((${pg_pss:-0} + share))
	done

	# The first line of /proc/stat is all CPUs together; its fourth number
	# is idle time. There is one "cpuN" line for each CPU after it.
	cpus=0 idle=0
	while read -r name _ _ _ idle_ticks _; do
		case $name in
		cpu) idle=$idle_ticks ;;
		cpu[0-9]*) cpus=$((cpus + 1)) ;;
		*) break ;;
		esac
	done <"$PROC/stat"

	read -r rx <"$SYS/class/net/$IFACE/statistics/rx_bytes"
	read -r tx <"$SYS/class/net/$IFACE/statistics/tx_bytes"

	echo "$now,$(seconds "$churr_ticks"),$(seconds "$pg_ticks"),$churr_rss,$pg_rss,$pg_pss,$(seconds "$idle"),$cpus,$rx,$tx" >>"$OUT"

	if [ -n "$LIMIT" ] && [ $((now - began)) -ge "$LIMIT" ]; then
		finish
	fi
	if [ ! -d "$PROC/$PID" ]; then
		echo "sample.sh: the churr process has ended" >&2
		finish
	fi
	sleep "$EVERY" &
	wait $!
done
