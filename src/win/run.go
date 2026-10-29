//go:build windows

package win

import (
	"bytes"
	"context"
	"os/exec"
	"syscall"
	"time"
)

// RunHidden はウィンドウを出さずにコマンドを実行して、標準出力を返す。
func RunHidden(exe, dir string, timeout time.Duration, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, exe, args...)
	cmd.Dir = dir
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	var out bytes.Buffer
	cmd.Stdout = &out
	err := cmd.Run()
	return out.String(), err
}
