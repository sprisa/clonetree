#!/usr/bin/env bash
# Record the README demo (light and dark) and the terminal social preview with
# VHS, and check the light recording's frames for layout jumps.
#
#   brew install vhs        # installs ttyd and ffmpeg too
#   bash docs/demo/record.sh
#
# See setup.sh for the sample project.
set -euo pipefail
cd "$(dirname "$0")/../.."

source docs/demo/setup.sh
# Both themes start from the same progress cache, so they show the same frames.
cache="$CLONETREE_DEMO/home/Library/Caches/clonetree"
cp -R "$cache" "$CLONETREE_DEMO/cache-before"

vhs docs/demo/demo.tape
# Reject recordings whose live frames jump: the same layout rules as the
# pseudo-terminal test in e2e/layout_test.go.
CLONETREE_UI_FRAMES="$PWD/docs/demo/.frames/demo.txt" GOWORK=off \
	go test -count=1 -run '^TestRecordedFramesAreStable$' ./e2e
# The social tape runs after the demo, so my-feature is listed too.
vhs docs/demo/social.tape
rm -f docs/demo/.social.gif

# Reset to the starting state, then record the dark theme.
(
	cd "$CLONETREE_DEMO/acme-app"
	HOME="$CLONETREE_DEMO/home" "$CLONETREE_DEMO/bin/clonetree" --no-progress delete my-feature >/dev/null
)
rm -rf "$cache" && cp -R "$CLONETREE_DEMO/cache-before" "$cache"
vhs docs/demo/demo-dark.tape
echo "Wrote docs/assets/demo.gif, docs/assets/demo-dark.gif, and docs/assets/social-terminal.png"
