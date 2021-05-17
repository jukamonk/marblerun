// Copyright (c) Edgeless Systems GmbH.
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package main

import (
	"os"
	"syscall"

	marblePremain "github.com/edgelesssys/marblerun/marble/premain"
	"github.com/spf13/afero"
)

func main() {
	// save the passed argument which is our service to spawn
	service := os.Args[0]

	hostfs := afero.NewOsFs()
	if err := marblePremain.PreMainEx(marblePremain.GrapheneQuoteIssuer{}, marblePremain.GrapheneActivate, hostfs, hostfs); err != nil {
		panic(err)
	}

	// launch service
	if err := syscall.Exec(service, os.Args, os.Environ()); err != nil {
		panic(err)
	}
}
