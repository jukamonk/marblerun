// Copyright (c) Edgeless Systems GmbH.
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package main

import (
	"log"
	"os"
	"strings"

	"github.com/edgelesssys/marblerun/coordinator/config"
	"github.com/edgelesssys/marblerun/coordinator/core"
	"github.com/edgelesssys/marblerun/coordinator/quote"
	"github.com/edgelesssys/marblerun/coordinator/recovery"
	"github.com/edgelesssys/marblerun/coordinator/server"
	"github.com/edgelesssys/marblerun/util"
	"go.uber.org/zap"
)

// Version is the Coordinator version
var Version = "0.0.0" // Don't touch! Automatically injected at build-time.

// GitCommit is the git commit hash
var GitCommit string // Don't touch! Automatically injected at build-time.

func run(validator quote.Validator, issuer quote.Issuer, sealDir string, sealer core.Sealer, recovery recovery.Recovery) {
	// Setup logging with Zap Logger
	var zapLogger *zap.Logger
	var err error

	// Development Logger shows a stacktrace for warnings & errors, Production Logger only for errors
	devMode := util.Getenv(config.DevMode, config.DevModeDefault)
	if devMode == "1" {
		zapLogger, err = zap.NewDevelopment()
	} else {
		zapLogger, err = zap.NewProduction()
	}
	if err != nil {
		log.Fatal(err)
	}
	defer zapLogger.Sync() // flushes buffer, if any

	zapLogger.Info("starting coordinator", zap.String("version", Version), zap.String("commit", GitCommit))

	// fetching env vars
	dnsNamesString := util.Getenv(config.DNSNames, config.DNSNamesDefault)
	dnsNames := strings.Split(dnsNamesString, ",")
	clientServerAddr := util.Getenv(config.ClientAddr, config.ClientAddrDefault)
	meshServerAddr := util.Getenv(config.MeshAddr, config.MeshAddrDefault)
	promServerAddr := os.Getenv(config.PromAddr)

	// creating core
	zapLogger.Info("creating the Core object")
	if err := os.MkdirAll(sealDir, 0700); err != nil {
		zapLogger.Fatal("Cannot create or access sealdir. Please check the permissions for the specified path.", zap.Error(err))
	}
	core, err := core.NewCore(dnsNames, validator, issuer, sealer, recovery, zapLogger)
	if err != nil {
		panic(err)
	}

	// start the prometheus server
	if promServerAddr != "" {
		go server.RunPrometheusServer(promServerAddr, zapLogger)
	}

	// start client server
	zapLogger.Info("starting the client server")
	mux := server.CreateServeMux(core)
	clientServerTLSConfig, err := core.GetTLSConfig()
	if err != nil {
		panic(err)
	}
	go server.RunClientServer(mux, clientServerAddr, clientServerTLSConfig, zapLogger)

	// run marble server
	zapLogger.Info("starting the marble server")
	addrChan := make(chan string)
	errChan := make(chan error)
	go server.RunMarbleServer(core, meshServerAddr, addrChan, errChan, zapLogger)
	for {
		select {
		case err := <-errChan:
			if err != nil {
				panic(err)
			}
			return
		case grpcAddr := <-addrChan:
			zapLogger.Info("started gRPC server", zap.String("grpcAddr", grpcAddr))
		}
	}
}
