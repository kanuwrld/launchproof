#!/bin/sh
set -eu
mkdir -p work/previews
while read -r name width height; do
  tape="work/previews/$name.tape"
  cat > "$tape" <<EOF
Output work/previews/$name.gif
Set Width $width
Set Height $height
Source docs/theme.tape
Hide
Type "./bin/launchproof --no-motion"
Enter
Sleep 1s
Show
Sleep 1s
Screenshot assets/welcome-$name.png
Sleep 1s
Type "q"
EOF
  vhs "$tape"
done <<'SIZES'
square 960 960
portrait 720 1080
7-6 1120 960
short 1280 360
SIZES
