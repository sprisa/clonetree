#!/usr/bin/env bash
# Record the README demo and the terminal social preview with VHS, and check
# the recorded frames for layout jumps.
#
#   brew install vhs        # installs ttyd and ffmpeg too
#   bash docs/demo/record.sh
#
# See setup.sh for the sample project.
set -euo pipefail
cd "$(dirname "$0")/../.."

source docs/demo/setup.sh

vhs docs/demo/demo.tape
# Reject recordings whose live frames jump: the same layout rules as the
# pseudo-terminal test in e2e/layout_test.go.
CLONETREE_UI_FRAMES="$PWD/docs/demo/.frames/demo.txt" GOWORK=off \
	go test -count=1 -run '^TestRecordedFramesAreStable$' ./e2e
# The social tape runs after the demo, so my-feature is listed too.
vhs docs/demo/social.tape
rm -f docs/demo/.social.gif
echo "Wrote docs/assets/demo.gif and docs/assets/social-terminal.png"
