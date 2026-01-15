// Package main provides a minimal test to verify purego works from installed locations.
// This validates the Stage 1 verification step for ADR-022.
package main

import (
	"fmt"
	"os"
	"runtime"

	"github.com/ebitengine/purego"
)

func main() {
	fmt.Println("purego verification test")
	fmt.Printf("OS: %s, Arch: %s\n", runtime.GOOS, runtime.GOARCH)

	// Test 1: Verify purego can load system library
	// On macOS, libSystem.dylib is always available
	// On Linux, libc.so.6 is always available
	var libPath string
	switch runtime.GOOS {
	case "darwin":
		libPath = "/usr/lib/libSystem.B.dylib"
	case "linux":
		libPath = "libc.so.6"
	default:
		fmt.Printf("Unsupported OS: %s\n", runtime.GOOS)
		os.Exit(1)
	}

	fmt.Printf("Loading system library: %s\n", libPath)

	lib, err := purego.Dlopen(libPath, purego.RTLD_NOW|purego.RTLD_GLOBAL)
	if err != nil {
		fmt.Printf("ERROR: Failed to load library: %v\n", err)
		os.Exit(1)
	}
	defer purego.Dlclose(lib)

	fmt.Println("SUCCESS: Library loaded successfully")

	// Test 2: Verify we can get a symbol
	var getpid func() int32
	purego.RegisterLibFunc(&getpid, lib, "getpid")

	pid := getpid()
	fmt.Printf("Current PID (via purego): %d\n", pid)
	fmt.Printf("Current PID (via Go): %d\n", os.Getpid())

	if int(pid) != os.Getpid() {
		fmt.Println("ERROR: PID mismatch!")
		os.Exit(1)
	}

	fmt.Println("\nVERIFICATION PASSED: purego works correctly")
	fmt.Println("This binary can be installed anywhere and will still work.")
}
