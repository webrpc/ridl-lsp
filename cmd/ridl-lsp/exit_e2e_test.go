package main

import (
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestExitNotificationExitCode runs the real binary to pin the LSP exit-code
// contract end-to-end: exit-without-shutdown is 1, shutdown-then-exit is 0. This
// is the only level that exercises the read-loop ordering — a unit test on
// Server.Exit can't see the EOF/async race that an in-process handler would hit.
func TestExitNotificationExitCode(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "ridl-lsp-test")
	if out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput(); err != nil {
		t.Fatalf("build binary: %v\n%s", err, out)
	}

	frame := func(body string) string {
		return fmt.Sprintf("Content-Length: %d\r\n\r\n%s", len(body), body)
	}
	shutdown := frame(`{"jsonrpc":"2.0","id":1,"method":"shutdown","params":null}`)
	exit := frame(`{"jsonrpc":"2.0","method":"exit","params":null}`)

	cases := []struct {
		name  string
		input string
		want  int
	}{
		{"exit without shutdown", exit, 1},
		{"shutdown then exit", shutdown + exit, 0},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cmd := exec.Command(bin)
			cmd.Stdin = strings.NewReader(tc.input)

			got := 0
			if err := cmd.Run(); err != nil {
				var exitErr *exec.ExitError
				if !errors.As(err, &exitErr) {
					t.Fatalf("run binary: %v", err)
				}
				got = exitErr.ExitCode()
			}
			if got != tc.want {
				t.Fatalf("exit code: got %d, want %d", got, tc.want)
			}
		})
	}
}
