#!/usr/bin/env bash
set -euo pipefail

REPO_URL="${UPSTREAM_TC_REPO:-https://github.com/testcontainers/testcontainers-java.git}"
REPO_DIR="${UPSTREAM_TC_DIR:-/workspace/testcontainers-java}"
REPO_REF="${UPSTREAM_TC_REF:-}"
FETCH_MODE="${UPSTREAM_TC_FETCH_MODE:-auto}" # auto|never
BUNDLED_REPO_DIR="${UPSTREAM_TC_BUNDLED_DIR:-/opt/testcontainers-java}"
TASK="${UPSTREAM_TC_TASK:-:testcontainers:test}"
TEST_ARGS_RAW="${UPSTREAM_TC_TEST_ARGS---tests org.testcontainers.containers.ContainerStateTest}"
EXTRA_GRADLE_ARGS_RAW="${UPSTREAM_TC_EXTRA_GRADLE_ARGS---rerun-tasks --max-workers=1 --no-daemon}"
EXPECTED_TESTS="${UPSTREAM_TC_EXPECTED_TESTS:-}"

export DOCKER_HOST="${DOCKER_HOST:-tcp://${TESTTENDER_SERVICE_HOST:-testtender}:2475}"
export TESTCONTAINERS_RYUK_DISABLED="${TESTCONTAINERS_RYUK_DISABLED:-true}"
export TESTCONTAINERS_CHECKS_DISABLE="${TESTCONTAINERS_CHECKS_DISABLE:-true}"
export GRADLE_USER_HOME="${GRADLE_USER_HOME:-/opt/gradle-home}"

mkdir -p "$(dirname "$REPO_DIR")" "$GRADLE_USER_HOME"

if [ ! -d "$REPO_DIR/.git" ] && [ -d "$BUNDLED_REPO_DIR/.git" ]; then
  echo "[runner] seeding repo from bundled source $BUNDLED_REPO_DIR"
  cp -a "$BUNDLED_REPO_DIR" "$REPO_DIR"
fi

if [ ! -d "$REPO_DIR/.git" ]; then
  if [ "$FETCH_MODE" = "never" ]; then
    echo "[runner] fetch mode is 'never' and no bundled repo is present"
    exit 1
  fi
  echo "[runner] cloning $REPO_URL -> $REPO_DIR"
  git clone --depth=1 "$REPO_URL" "$REPO_DIR"
fi

if [ "$FETCH_MODE" = "never" ]; then
  echo "[runner] fetch mode is 'never' (using local repo state)"
elif [ -n "$REPO_REF" ]; then
  echo "[runner] fetching ref $REPO_REF"
  git -C "$REPO_DIR" fetch --depth=1 origin "$REPO_REF"
  git -C "$REPO_DIR" checkout --force FETCH_HEAD
else
  echo "[runner] refreshing default branch"
  git -C "$REPO_DIR" fetch --depth=1 origin
  git -C "$REPO_DIR" reset --hard origin/HEAD
fi

echo "[runner] DOCKER_HOST=$DOCKER_HOST"
echo "[runner] TASK=$TASK"
echo "[runner] TEST_ARGS=$TEST_ARGS_RAW"
echo "[runner] EXTRA_GRADLE_ARGS=$EXTRA_GRADLE_ARGS_RAW"
if [ -n "$EXPECTED_TESTS" ]; then
  echo "[runner] EXPECTED_TESTS=$EXPECTED_TESTS"
fi

if ! curl -fsS "${DOCKER_HOST/tcp:\/\//http://}/_ping" >/dev/null; then
  echo "[runner] testtender not reachable at $DOCKER_HOST"
  exit 1
fi

TEST_ARGS=()
EXTRA_GRADLE_ARGS=()
if [ -n "$TEST_ARGS_RAW" ]; then
  read -r -a TEST_ARGS <<< "$TEST_ARGS_RAW"
fi
if [ -n "$EXTRA_GRADLE_ARGS_RAW" ]; then
  read -r -a EXTRA_GRADLE_ARGS <<< "$EXTRA_GRADLE_ARGS_RAW"
fi

cd "$REPO_DIR"
set -x
set +e
./gradlew "$TASK" "${TEST_ARGS[@]}" "${EXTRA_GRADLE_ARGS[@]}"
gradle_status=$?
set -e
set +x

if [ -n "$EXPECTED_TESTS" ]; then
  result_dir="${UPSTREAM_TC_RESULT_DIR:-$REPO_DIR/core/build/test-results/test}"
  actual_tests="$({
    find "$result_dir" -maxdepth 1 -type f -name 'TEST-*.xml' -exec awk '
      match($0, /tests="[0-9]+"/) {
        value = substr($0, RSTART + 7, RLENGTH - 8)
        total += value
      }
      END { print total + 0 }
    ' {} +
  } 2>/dev/null || true)"
  actual_tests="${actual_tests:-0}"
  if [ "$actual_tests" != "$EXPECTED_TESTS" ]; then
    echo "[runner] expected $EXPECTED_TESTS selected tests, but Gradle reported $actual_tests" >&2
    exit 2
  fi
fi

exit "$gradle_status"
