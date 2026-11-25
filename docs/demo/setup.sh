#!/usr/bin/env bash
# Prepare the sample project used by the VHS tapes. Source this from the
# repository root; it exports CLONETREE_DEMO and removes it on exit.
#
# Builds clonetree, creates a sample project with installed dependencies in a
# temporary directory on this filesystem, pre-creates four snapshots (and one
# info run, so recordings show progress percentages). HOME is redirected inside
# the recording shell so the user cache is not touched.
command -v vhs >/dev/null || { echo "vhs not found: brew install vhs" >&2; exit 1; }

export CLONETREE_DEMO
CLONETREE_DEMO=$(mktemp -d "${TMPDIR:-/tmp}/clonetree-demo.XXXXXX")
trap 'chmod -R u+w "$CLONETREE_DEMO" 2>/dev/null; rm -rf "$CLONETREE_DEMO"' EXIT
mkdir -p "$CLONETREE_DEMO/bin" "$CLONETREE_DEMO/home"
cat >"$CLONETREE_DEMO/env.sh" <<'SH'
export HOME="$CLONETREE_DEMO/home" PATH="$CLONETREE_DEMO/bin:$PATH" COLORTERM=truecolor
PS1="\[$(tput setaf 8)\]~/acme-app \[$(tput setaf 2)\]❯\[$(tput sgr0)\] "
cd "$CLONETREE_DEMO/acme-app"
banner() {
	printf '\n\033[1;38;2;75;60;50mclonetree\033[0m\n\033[38;2;101;116;93mCopy your repo, node_modules and all, in seconds.\033[0m\n\033[38;2;128;107;88mOnly changes take up disk space.\033[0m\n\n'
}
SH
GOWORK=off go build -trimpath -o "$CLONETREE_DEMO/bin/clonetree" .
GOWORK=off go run ./docs/demo/fixture "$CLONETREE_DEMO/acme-app" 900

(
	cd "$CLONETREE_DEMO/acme-app"
	export HOME="$CLONETREE_DEMO/home" PATH="$CLONETREE_DEMO/bin:$PATH"
	clonetree init >/dev/null
	for id in main release-2-4 hotfix-login perf-experiment; do
		clonetree --no-progress new "$id" >/dev/null
	done
	# Give the snapshots some private (unshared) data, as real work would.
	head -c 180000000 /dev/urandom >clonetree/perf-experiment.clonetree/dist/profile.bin
	head -c 24000000 /dev/urandom >clonetree/hotfix-login.clonetree/apps/web/src/login.ts
	head -c 9000000 /dev/urandom >clonetree/main.clonetree/dist/chunk-0.js
	# Seed entry-count estimates; they are only recorded by interactive runs.
	script -q /dev/null clonetree info >/dev/null
)
