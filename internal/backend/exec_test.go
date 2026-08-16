package backend

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/obegron/testtender/internal/model/types"
)

func TestParseExecResponse(t *testing.T) {
	tests := []struct {
		in  error
		cod int
		suc bool
	}{
		{nil, 0, true},
		{fmt.Errorf("some generic error"), 0, false},
		{fmt.Errorf("command terminated with exit code 2"), 2, true},
	}

	for i, tst := range tests {
		kub := &instance{}
		cod, err := kub.parseExecResponse(tst.in)
		if cod != tst.cod {
			t.Errorf("failed test %d - expected %d, but got %d", i, tst.cod, cod)
		}
		if err != nil && tst.suc {
			t.Errorf("failed test %d - unexpected error: %s", i, err)
		}
		if err == nil && !tst.suc {
			t.Errorf("failed test %d - expected error, but succeeded instead", i)
		}
	}
}

func TestPrepareExecCommand(t *testing.T) {
	tests := []struct {
		name string
		exec *types.Exec
		want []string
	}{
		{
			name: "plain",
			exec: &types.Exec{Cmd: []string{"redis-cli", "role"}},
			want: []string{"redis-cli", "role"},
		},
		{
			name: "environment",
			exec: &types.Exec{Cmd: []string{"env"}, Env: []string{"TESTCONTAINERS=JAVA", "EMPTY="}},
			want: []string{"env", "TESTCONTAINERS=JAVA", "EMPTY=", "env"},
		},
		{
			name: "working directory and user",
			exec: &types.Exec{Cmd: []string{"whoami"}, User: "redis", WorkingDir: "/opt"},
			want: []string{"/bin/sh", "-c", execContextScript, "testtender-exec", "redis", "/opt", "whoami"},
		},
		{
			name: "all overrides",
			exec: &types.Exec{Cmd: []string{"pwd"}, Env: []string{"A=b=c"}, User: "1000:1000", WorkingDir: "/tmp/a b"},
			want: []string{"env", "A=b=c", "/bin/sh", "-c", execContextScript, "testtender-exec", "1000:1000", "/tmp/a b", "pwd"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := prepareExecCommand(tt.exec)
			if err != nil {
				t.Fatalf("prepareExecCommand() error = %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("prepareExecCommand() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestPrepareExecCommandRejectsInvalidContext(t *testing.T) {
	tests := []*types.Exec{
		{},
		{Cmd: []string{"true"}, Env: []string{"MISSING_VALUE"}},
		{Cmd: []string{"true"}, Env: []string{"-OPTION=value"}},
		{Cmd: []string{"true"}, User: "-root"},
		{Cmd: []string{"true"}, WorkingDir: "bad\x00path"},
	}
	for _, ex := range tests {
		if _, err := prepareExecCommand(ex); err == nil {
			t.Errorf("expected invalid exec context %#v to fail", ex)
		}
	}
}
