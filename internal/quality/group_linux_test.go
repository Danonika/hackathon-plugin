//go:build linux

package quality

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestPinnedGroupCanCleanUpAfterLeaderExit(t *testing.T) {

	for _, tc := range []struct {
		name  string
		pin   func(*exec.Cmd) (func(), func())
		probe bool
	}{{"native", ownGroup, true}, {"portable", pinGroupMember, false}} {
		t.Run(tc.name, func(t *testing.T) {
			cmd := exec.Command("sh", "-c", "sleep 15 >/dev/null 2>&1 & echo $!")
			var out bytes.Buffer
			cmd.Stdout = &out
			configureProcessGroup(cmd)
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			kill, release := tc.pin(cmd)
			defer release()
			supported := false
			_ = cmd.Process.WithHandle(func(h uintptr) { supported = unix.PidfdSendSignal(int(h), 0, nil, pidfdSignalProcessGroup) == nil })
			if err := cmd.Wait(); err != nil {
				kill()
				t.Fatal(err)
			}
			child, err := strconv.Atoi(strings.TrimSpace(out.String()))
			if err != nil {
				kill()
				t.Fatal(err)
			}
			proc, err := os.FindProcess(child)
			if err != nil {
				kill()
				t.Fatal(err)
			}
			defer proc.Release()
			defer proc.Kill()
			kill()
			if tc.probe && !supported {
				t.Skip("kernel lacks process-group pidfd signaling")
			}
			deadline := time.Now().Add(time.Second)
			for time.Now().Before(deadline) {
				b, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", child))
				if os.IsNotExist(err) || strings.Contains(string(b), ") Z ") {
					return
				}
				time.Sleep(10 * time.Millisecond)
			}
			t.Fatal("child survived the original process group")
		})
	}
}
