#!/bin/bash
# Rehearse a phone-triggered relay update the way it really happens on a
# computer: the update worker runs under a service manager's bare environment
# (no user bin directory on PATH, only the variables the relay forwards), asks
# the Herdr CLI to reinstall the plugin, and the real plugin build and installer
# download a release from the repository the relay was installed from. GitHub
# is replaced by a local HTTP server that serves a fork-named release.
#
# Every failure this guards against has happened once: the worker downloading
# from upstream instead of the fork, the build unable to find the Herdr CLI
# without PATH, and the service's env file missing the repository.
set -euo pipefail

REPO_DIR="$(cd "$(dirname "$0")/.." && pwd)"
WORK="$(mktemp -d "${TMPDIR:-/tmp}/herdr-update-rehearsal.XXXXXX")"
SERVER_PID=""
SERVICE_PID=""
cleanup() {
    [ -z "$SERVICE_PID" ] || kill "$SERVICE_PID" 2>/dev/null || true
    [ -z "$SERVER_PID" ] || kill "$SERVER_PID" 2>/dev/null || true
    pkill -P $$ 2>/dev/null || true
    # HERDR_REHEARSAL_KEEP=1 leaves the work directory behind for inspection.
    [ -n "${HERDR_REHEARSAL_KEEP:-}" ] && { echo "kept $WORK" >&2; return; }
    # Sealed releases are read-only; make the tree deletable first.
    chmod -R u+w "$WORK" 2>/dev/null || true
    rm -rf "$WORK"
}
trap cleanup EXIT

fail() {
    echo "FAIL: $*" >&2
    echo "--- worker output ---" >&2
    cat "$WORK/worker.log" 2>/dev/null >&2 || true
    echo "--- stand-in service output ---" >&2
    cat "$WORK/service.log" 2>/dev/null >&2 || true
    echo "--- update state ---" >&2
    cat "$CONFIG_DIR/update-state.json" 2>/dev/null >&2 || true
    echo >&2
    exit 1
}

VERSION="$(sed -n 's/^version = "\([^"]*\)"/\1/p' "$REPO_DIR/herdr-plugin.toml")"
NEXT="${VERSION%.*}.$(( ${VERSION##*.} + 1 ))"
CURRENT_REVISION="$(git -C "$REPO_DIR" rev-parse HEAD 2>/dev/null || printf 'c%.0s' $(seq 40))"
NEXT_REVISION="$(head -c 20 /dev/urandom | od -An -tx1 | tr -d ' \n')"
HOST_TARGET="$(go env GOOS)/$(go env GOARCH)"
ARCHIVE_TARGET="$(go env GOOS)_$(go env GOARCH)"
REPOSITORY="rehearsal/herdr-mobile-relay-fork"

HOME_DIR="$WORK/home"
DATA_HOME="$HOME_DIR/.local/share"
RELEASE_ROOT="$DATA_HOME/herdr-mobile-relay"
CONFIG_DIR="$HOME_DIR/.config/herdr/plugins/config/herdr-mobile-relay.events"
FAKE_BIN="$HOME_DIR/.local/bin"
SITE="$WORK/site"
PORT=$((41000 + ($$ % 10000)))

mkdir -p "$FAKE_BIN" "$RELEASE_ROOT/releases" "$CONFIG_DIR" "$HOME_DIR/.cache" \
    "$SITE/releases/download/v$NEXT" "$SITE/repos/$REPOSITORY/commits"

echo "▸ Packaging the installed release $VERSION and the next release $NEXT ($HOST_TARGET)"
HERDR_RELEASE_TARGETS="$HOST_TARGET" "$REPO_DIR/scripts/package-release.sh" \
    "$VERSION" "$CURRENT_REVISION" "$WORK/current-bundle" >/dev/null
HERDR_RELEASE_TARGETS="$HOST_TARGET" "$REPO_DIR/scripts/package-release.sh" \
    "$NEXT" "$NEXT_REVISION" "$SITE/releases/download/v$NEXT" >/dev/null

# The installed release, laid out the way the installer leaves it.
CURRENT_DIR="$RELEASE_ROOT/releases/$VERSION-$CURRENT_REVISION-$ARCHIVE_TARGET"
mkdir -p "$CURRENT_DIR"
tar -xzf "$WORK/current-bundle/herdr-mobile-relay_${VERSION}_${ARCHIVE_TARGET}.tar.gz" -C "$CURRENT_DIR"
ln -s "$CURRENT_DIR" "$RELEASE_ROOT/current"
# The installer only touches roots it owns; a real install leaves these.
for owned in "$RELEASE_ROOT" "$CONFIG_DIR" "$HOME_DIR/.cache/herdr-mobile-relay"; do
    mkdir -p "$owned"
    printf 'product=herdr-mobile-relay\nroot=%s\n' "$(cd "$owned" && pwd -P)" > "$owned/.herdr-mobile-relay-installation"
done
RELAY="$RELEASE_ROOT/current/herdr-mobile-relay"

# The stand-in for GitHub: the tag's commit for the installer and the release
# assets for both the worker and the installer.
printf '{"sha":"%s"}\n' "$NEXT_REVISION" > "$SITE/repos/$REPOSITORY/commits/v$NEXT"
python3 -u -m http.server 0 --bind 127.0.0.1 --directory "$SITE" > "$WORK/server.log" 2>&1 &
SERVER_PID=$!
for _ in $(seq 1 50); do
    SITE_PORT="$(sed -n 's/.*port \([0-9][0-9]*\).*/\1/p' "$WORK/server.log" | head -1)"
    [ -n "$SITE_PORT" ] && break
    sleep 0.1
done
[ -n "$SITE_PORT" ] || fail "the GitHub stand-in did not start"
SITE_URL="http://127.0.0.1:$SITE_PORT"

# The plugin checkout Herdr would fetch at the next release's commit: this
# tree's build and installer, with the manifest naming the next version.
CHECKOUT="$WORK/checkout"
mkdir -p "$CHECKOUT"
cp -R "$REPO_DIR/relay" "$CHECKOUT/relay"
cp "$REPO_DIR/install.sh" "$CHECKOUT/install.sh"
sed "s/^version = \"$VERSION\"/version = \"$NEXT\"/" "$REPO_DIR/herdr-plugin.toml" > "$CHECKOUT/herdr-plugin.toml"
grep -q "^version = \"$NEXT\"" "$CHECKOUT/herdr-plugin.toml" || fail "could not stage the next manifest"

# The relay's configuration, as the plugin build leaves it.
printf "HERDR_RELAY_TOKEN='rehearsal-token-0123456789abcdef'\nHERDR_RELAY_PORT='%s'\n" "$PORT" > "$CONFIG_DIR/relay.env"

# The Herdr CLI stand-in lives outside PATH on purpose: the worker must name
# it. Its plugin install runs the real build from the staged checkout, and the
# build must ask it for the plugin's config directory.
cat > "$FAKE_BIN/herdr" <<HERDR
#!/bin/bash
set -eu
export XDG_DATA_HOME="$DATA_HOME"
export HERDR_PLUGIN_INSTALLER="$CHECKOUT/install.sh"
export HERDR_RELEASE_BASE_URL="$SITE_URL/releases/download/v$NEXT"
export HERDR_RELEASE_API_BASE="$SITE_URL"
case "\${1:-} \${2:-}" in
    "plugin config-dir")
        echo "$CONFIG_DIR"
        ;;
    "plugin install")
        shift 2
        repository="\${1:-}"
        shift
        ref=""
        while [ \$# -gt 0 ]; do
            case "\$1" in
                --ref) ref="\$2"; shift 2 ;;
                *) shift ;;
            esac
        done
        [ "\$repository" = "$REPOSITORY" ] || { echo "herdr stand-in: unexpected repository \$repository" >&2; exit 1; }
        [ "\$ref" = "$NEXT_REVISION" ] || { echo "herdr stand-in: unexpected ref \$ref" >&2; exit 1; }
        cd "$CHECKOUT"
        exec bash relay/plugin-build.sh
        ;;
    "session list")
        echo '[]'
        ;;
    *)
        exit 0
        ;;
esac
HERDR
chmod 700 "$FAKE_BIN/herdr"

# The service manager stand-in: once the build has cut "current" over to the
# next release, it starts that relay, which is what the worker's health check
# waits for.
(
    while sleep 1; do
        active="$(sed -n 's/.*"version":[[:space:]]*"\([^"]*\)".*/\1/p' "$RELEASE_ROOT/current/release-manifest.json" 2>/dev/null | head -1)"
        if [ "$active" = "$NEXT" ]; then
            exec env -i HOME="$HOME_DIR" PATH="/usr/bin:/bin" \
                XDG_DATA_HOME="$DATA_HOME" XDG_CACHE_HOME="$HOME_DIR/.cache" \
                HERDR_RELAY_ENV="$CONFIG_DIR/relay.env" HERDR_BIN="$FAKE_BIN/herdr" \
                HERDR_RELAY_HOST=127.0.0.1 HERDR_RELAY_PORT="$PORT" \
                "$RELEASE_ROOT/current/herdr-mobile-relay" serve
        fi
    done
) > "$WORK/service.log" 2>&1 &
SERVICE_PID=$!

cat > "$WORK/job.json" <<JOB
{
  "release_root": "$RELEASE_ROOT",
  "herdr_bin": "$FAKE_BIN/herdr",
  "target_version": "$NEXT",
  "target_revision": "$NEXT_REVISION",
  "state_path": "$CONFIG_DIR/update-state.json",
  "health_url": "http://127.0.0.1:$PORT/healthz"
}
JOB

echo "▸ Running the update worker $VERSION → $NEXT under a bare environment"
# Only what the relay forwards to the worker, and nothing from this shell.
if ! env -i HOME="$HOME_DIR" PATH="/usr/local/bin:/usr/bin:/bin" \
    HERDR_RELAY_ENV="$CONFIG_DIR/relay.env" \
    HERDR_RELEASE_REPOSITORY="$REPOSITORY" \
    HERDR_RELEASE_DOWNLOAD_BASE="$SITE_URL/releases/download" \
    HERDR_RELEASE_API_BASE="$SITE_URL" \
    HERDR_RELEASE_BASE_URL="$SITE_URL/releases/download/v$NEXT" \
    "$RELAY" update-worker "$WORK/job.json" > "$WORK/worker.log" 2>&1; then
    fail "the update worker exited with status $?"
fi

STATE="$CONFIG_DIR/update-state.json"
grep -q '"state":[[:space:]]*"succeeded"' "$STATE" || fail "update state is not succeeded: $(cat "$STATE")"
grep -q "\"current_version\":[[:space:]]*\"$NEXT\"" "$STATE" || fail "update state does not report $NEXT: $(cat "$STATE")"
case "$(readlink "$RELEASE_ROOT/current")" in
    *"$NEXT-$NEXT_REVISION-"*) ;;
    *) fail "current does not point at the next release: $(readlink "$RELEASE_ROOT/current")" ;;
esac
grep -q "^HERDR_RELEASE_REPOSITORY='$REPOSITORY'" "$CONFIG_DIR/relay.env" ||
    fail "the relay env does not record the install repository: $(cat "$CONFIG_DIR/relay.env")"
"$RELEASE_ROOT/current/herdr-mobile-relay" version | grep -q "$NEXT ($NEXT_REVISION)" ||
    fail "the activated relay is not $NEXT"
echo "update rehearsal passed: $VERSION → $NEXT from $REPOSITORY"
