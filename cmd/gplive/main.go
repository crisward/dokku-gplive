// gplive collects dokku server state and stats for gitpushlive.com.
package main

import (
	"fmt"
	"os"
)

var version = "dev"

func main() {
	cmd := ""
	if len(os.Args) > 1 {
		cmd = os.Args[1]
	}
	var err error
	switch cmd {
	case "stats":
		err = sendStats()
	case "state":
		err = getState(false)
	case "getstate":
		err = getState(true)
	case "disk-refresh":
		err = diskRefresh()
	case "version":
		fmt.Println(version)
	default:
		fmt.Fprintln(os.Stderr, "usage: gplive stats|state|getstate|disk-refresh|version")
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "gplive:", err)
		os.Exit(1)
	}
}
