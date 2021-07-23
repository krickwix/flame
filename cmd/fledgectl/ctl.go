package main

import (
	"os"

	"go.uber.org/zap"

	"wwwin-github.cisco.com/eti/fledge/cmd/fledgectl/cmd"
	"wwwin-github.cisco.com/eti/fledge/pkg/util"
)

func main() {
	loggerMgr := util.InitZapLog(util.CliTool)
	zap.ReplaceGlobals(loggerMgr)
	defer loggerMgr.Sync()

	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}