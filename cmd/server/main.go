package main

import (
	"log"

	"github.com/pessolato/httpmicrobench/pkg/osutil"
	"github.com/pessolato/httpmicrobench/pkg/server"
)

func main() {
	port := "8080"
	osutil.ExitOnErr(
		osutil.Load(
			osutil.NewEnvVar("TEST_SERVER_PORT", &port, false),
		))

	log.Printf("starting server at port %s ...", port)
	osutil.ExitOnErr(server.ListenAndServeRand(":" + port))
}
