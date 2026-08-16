package backend

import (
	"context"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
	"sync"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/obegron/testtender/internal/model/types"
	"github.com/obegron/testtender/internal/util/exec"
	"github.com/obegron/testtender/internal/util/ioproxy"
)

// ExecContainer will execute given exec object in kubernetes.
func (in *instance) ExecContainer(tainr *types.Container, ex *types.Exec, stdin io.Reader, stdout io.Writer) (int, error) {
	pod, err := in.cli.CoreV1().Pods(in.namespace).Get(context.Background(), tainr.GetPodName(), metav1.GetOptions{})
	if err != nil {
		return 0, err
	}

	cmd, err := prepareExecCommand(ex)
	if err != nil {
		return 0, err
	}

	req := exec.Request{
		Client:     in.cli,
		RestConfig: in.cfg,
		Pod:        *pod,
		Container:  "main",
		Cmd:        cmd,
		TTY:        ex.TTY,
	}

	if ex.Stdin {
		req.Stdin = stdin
	}
	if ex.TTY {
		req.Stdout = stdout
		req.Stderr = io.Discard
	} else {
		lock := sync.Mutex{}
		if ex.Stdout {
			iop := ioproxy.New(stdout, ioproxy.Stdout, &lock)
			req.Stdout = iop
			defer iop.Flush()
		}
		if ex.Stderr {
			iop := ioproxy.New(stdout, ioproxy.Stderr, &lock)
			req.Stderr = iop
			defer iop.Flush()
		}
	}

	err = exec.RemoteCmd(req)
	return in.parseExecResponse(err)
}

var (
	execEnvName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	execUser    = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.-]*(:[A-Za-z0-9_][A-Za-z0-9_.-]*)?$`)
)

const execContextScript = `user=$1
workdir=$2
shift 2
if [ -n "$workdir" ]; then
  cd "$workdir" || exit 126
fi
if [ -n "$user" ]; then
  for helper in gosu su-exec; do
    if command -v "$helper" >/dev/null 2>&1; then
      exec "$helper" "$user" "$@"
    fi
  done
  if command -v setpriv >/dev/null 2>&1; then
    run_user=${user%%:*}
    if [ "$run_user" != "$user" ]; then
      run_group=${user#*:}
    else
      run_group=$(id -g "$run_user") || exit 126
    fi
    exec setpriv --reuid "$run_user" --regid "$run_group" --init-groups "$@"
  fi
  if command -v runuser >/dev/null 2>&1; then
    exec runuser -u "$user" -- "$@"
  fi
  echo "cannot execute as user $user: image has no supported user-switch helper" >&2
  exit 126
fi
exec "$@"`

// prepareExecCommand translates Docker exec context into a command Kubernetes
// can send through pods/exec. Kubernetes has no native exec environment,
// working-directory, or user fields, so these are applied by common tools in
// the target image. Values are positional arguments rather than shell source.
func prepareExecCommand(ex *types.Exec) ([]string, error) {
	if len(ex.Cmd) == 0 {
		return nil, fmt.Errorf("exec command is empty")
	}
	if strings.ContainsRune(ex.WorkingDir, '\x00') {
		return nil, fmt.Errorf("exec working directory contains a null byte")
	}
	if ex.User != "" && !execUser.MatchString(ex.User) {
		return nil, fmt.Errorf("invalid exec user %q", ex.User)
	}

	cmd := append([]string(nil), ex.Cmd...)
	if ex.User != "" || ex.WorkingDir != "" {
		cmd = append([]string{"/bin/sh", "-c", execContextScript, "testtender-exec", ex.User, ex.WorkingDir}, cmd...)
	}
	if len(ex.Env) == 0 {
		return cmd, nil
	}

	env := make([]string, 0, len(ex.Env))
	for _, value := range ex.Env {
		name, _, found := strings.Cut(value, "=")
		if !found || !execEnvName.MatchString(name) || strings.ContainsRune(value, '\x00') {
			return nil, fmt.Errorf("invalid exec environment entry %q", value)
		}
		env = append(env, value)
	}
	return append(append([]string{"env"}, env...), cmd...), nil
}

// parseExecResponse will take the given error and will parse the string to
// get an exit code from it. if no exit code is found, it will return 0 and
// the original error.
func (in *instance) parseExecResponse(err error) (int, error) {
	if err == nil {
		return 0, err
	}

	const eterm = "command terminated with exit code"
	if !strings.Contains(err.Error(), eterm) {
		return 0, err
	}

	cod, cerr := strconv.Atoi(strings.TrimPrefix(err.Error(), eterm+" "))
	if cerr != nil {
		return 0, err
	}

	return cod, nil
}
