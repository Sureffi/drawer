#!/bin/bash
# demo/record.sh — how demo/ was made. archbox: X11 under AwesomeWM, kitty,
# ffmpeg, maim, magick. A floating kitty on top, W x H (1280x860) at 12pt, runs
# claude as a fresh session (not a child of the one driving this); the end
# of the reply is read off DRAWER_TEE.
#
#   demo/record.sh session "PROMPT" [claude args...]   the video, mp4 and gif: the prompt typed in, the reply
#   demo/record.sh still RUNG         one still: demo/graph.dot fed back, on RUNG
#
# The session runs whatever drawer is installed — what a stranger has. A
# still runs this checkout, --plugin-dir with the user's settings off so
# the installed plugin stays out of it, on the rung DRAWER_RENDER names.
# Everything lands in demo/out/; the trim and the crops are at the end.
#
# A ~/.claude/CLAUDE.md is read by the session too, and the model may act
# on it before it draws; the video was taken with the house's moved aside.
set -e
here=$(cd "$(dirname "$0")" && pwd)
trap '[ -n "$kpid" ] && kill $kpid 2>/dev/null' EXIT
out=$here/out; mkdir -p "$out"
W=${W:-1280}; H=${H:-860}
sock=/run/user/$(id -u)/kitty-demo
K="kitten @ --to unix:$sock"

# launch NAME [claude args...] — kitty, floated and sized, its X window id in $wid
launch() {
	name=$1; shift
	rm -f "$out/$name.deltas.jsonl"
	DRAWER_TEE=$out/$name.deltas.jsonl kitty --class drawer-demo --title drawer-demo \
		-o allow_remote_control=yes --listen-on "unix:$sock" \
		-o background_opacity=1 -o background_image=none -o font_size=12 \
		-o window_padding_width=12 -o remember_window_size=no \
		-o initial_window_width=$W -o initial_window_height=$H \
		-o confirm_os_window_close=0 \
		env -u CLAUDECODE -u CLAUDE_CODE_CHILD_SESSION -u CLAUDE_CODE_SESSION_ID \
		-u CLAUDE_PID -u CLAUDE_CODE_MESSAGING_SOCKET -u CLAUDE_CODE_BRIDGE_SESSION_ID \
		-u CLAUDE_CODE_MESSAGING_TOKEN -u CLAUDE_EFFORT -u CLAUDE_CODE_ENTRYPOINT \
		-u CLAUDE_CODE_EXECPATH \
		claude --model sonnet --effort medium "$@" >/dev/null 2>&1 &
	kpid=$!
	sleep 1.5
	wid=$(awesome-client <<-LUA 2>&1 | sed -n 's/.*"\(0x[0-9a-f]*\)".*/\1/p'
		for _, c in ipairs(client.get()) do
		  if c.class == "drawer-demo" then
		    c.floating = true; c.ontop = true; c.border_width = 0
		    c:geometry({x = 200, y = 185, width = $W, height = $H})
		    c:raise()
		    return string.format("0x%x", c.window)
		  end
		end
		return "none"
	LUA
	)
	[ -n "$wid" ] && [ "$wid" != none ] || { echo "no window" >&2; exit 1; }
	sleep 6
}

# reply_done NAME — the last delta of the reply has been handed to the hook
reply_done() {
	for _ in $(seq 150); do
		sleep 1
		grep -q '"final":true' "$out/$1.deltas.jsonl" 2>/dev/null && return 0
	done
	return 1
}

case $1 in
session)
	prompt=$2; shift 2
	launch session "$@"
	ffmpeg -y -loglevel error -f x11grab -framerate 30 -window_id $((wid)) -i "$DISPLAY" \
		-draw_mouse 0 -c:v libx264 -preset veryfast -crf 18 -pix_fmt yuv420p "$out/session-raw.mp4" &
	ff=$!
	t0=$(date +%s.%N)
	sleep 1.5
	for ((i = 0; i < ${#prompt}; i++)); do
		$K send-text -- "${prompt:$i:1}"
		sleep 0.018
	done
	sleep 0.8
	$K send-text -- '\r'
	sent=$(awk -v a="$(date +%s.%N)" -v b="$t0" 'BEGIN { printf "%.2f", a - b }')
	reply_done session
	sleep 4
	kill -INT $ff; wait $ff || true
	maim -i "$wid" "$out/session.png"
	kill $kpid
	# the trim: from 0.8s, the typing at 2x until the prompt was sent, then
	# real time
	echo "prompt sent at ${sent}s"
	ffmpeg -y -loglevel error -i "$out/session-raw.mp4" -filter_complex \
		"[0:v]trim=0.8:$sent,setpts=(PTS-STARTPTS)/2[a];[0:v]trim=$sent,setpts=PTS-STARTPTS[b];[a][b]concat=n=2:v=1:a=0,format=yuv420p[v]" \
		-map "[v]" -c:v libx264 -preset slow -crf 20 -movflags +faststart "$out/session.mp4"
	# the gif is what the README shows: a terminal is flat colour, so one
	# palette and no dither is the picture, at a megabyte
	ffmpeg -y -loglevel error -i "$out/session.mp4" -vf \
		"fps=12,split[a][b];[a]palettegen=stats_mode=diff[p];[b][p]paletteuse=dither=none:diff_mode=rectangle" \
		"$out/session.gif"
	;;
still)
	r=$2
	DRAWER_RENDER=$r launch "$r" --plugin-dir "$here/.." --setting-sources project,local \
		"Put exactly this graph in a \`\`\`dot fence, nothing else:

$(cat "$here/graph.dot")"
	reply_done "$r"
	sleep 2.5
	maim -i "$wid" "$out/$r-full.png"
	kill $kpid
	# the crop: the band below the reply's bullet, trimmed to the drawing,
	# 24px of ground around. The band is where the drawing sat in that take,
	# at 1280x720.
	case $r in cells) band=1280x140+0+370 ;; *) band=1280x260+0+240 ;; esac
	magick "$out/$r-full.png" -crop "$band" +repage -fuzz 6% -trim +repage \
		-bordercolor '#0a0a0a' -border 24 "$out/$r.png"
	;;
*)
	echo "usage: demo/record.sh session \"PROMPT\" | still pixels|cells" >&2
	exit 2
	;;
esac
