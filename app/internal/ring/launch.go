package ring

import (
	"fmt"
	"os"
	"strings"
	"syscall"
	"time"
)

// Loading the core as MiSTer's own tools do (launch.md "Loading the core",
// h264ring_launch_core): an MGL naming the rbf, and load_core for it in
// one write to MiSTer main's command FIFO, only when /tmp/CORENAME does
// not already identify the app core. The caller then waits for the heartbeat.
var (
	coreNameFile = "/tmp/CORENAME"
	mglFile      = "/tmp/h264ring.mgl"
	cmdFile      = "/dev/MiSTer_cmd"
	launchPoll   = 100 * time.Millisecond
)

// CoreLoaded reports whether MiSTer main says the H.264 core is loaded.
func CoreLoaded() bool {
	name, ok := CoreName()
	return ok && appCore(name)
}

// AppCoreName is the name the Jellyfin build of the decoder gives itself in
// its config string: what MiSTer publishes as the loaded core's name.
const AppCoreName = "MisterZine Jellyfin"

// appCore reports whether a core name is the app's core.
func appCore(name string) bool {
	return name == AppCoreName || name == "H264" // accept old installations during upgrade
}

// CoreName reads the name MiSTer main publishes for the currently loaded core.
func CoreName() (string, bool) {
	b, err := os.ReadFile(coreNameFile)
	if err != nil {
		return "", false
	}
	name, _, _ := strings.Cut(string(b), "\n")
	name = strings.TrimRight(name, "\r")
	return name, name != ""
}

// LaunchCore loads rbf (default _Other/H264) unless the core is loaded,
// and waits up to timeout for /tmp/CORENAME to identify it.
func LaunchCore(rbf string, timeout time.Duration) error {
	if CoreLoaded() {
		return nil
	}
	if rbf == "" {
		rbf = "_Other/H264"
	}
	mgl := "<mistergamedescription>\n  <rbf>" + rbf + "</rbf>\n</mistergamedescription>\n"
	if err := os.WriteFile(mglFile, []byte(mgl), 0o644); err != nil {
		return err
	}
	f, err := os.OpenFile(cmdFile, os.O_WRONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return fmt.Errorf("MiSTer main's command FIFO: %w", err)
	}
	_, err = f.Write([]byte("load_core " + mglFile + "\n")) // one command, one write
	f.Close()
	if err != nil {
		return fmt.Errorf("writing load_core: %w", err)
	}
	for deadline := time.Now().Add(timeout); time.Now().Before(deadline); {
		time.Sleep(launchPoll)
		if CoreLoaded() {
			return nil
		}
	}
	return fmt.Errorf("the core did not load within %s", timeout)
}
