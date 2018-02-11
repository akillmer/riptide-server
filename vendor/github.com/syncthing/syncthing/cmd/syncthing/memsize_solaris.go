// Copyright (C) 2014 The Syncthing Authors.
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this file,
// You can obtain one at https://mozilla.org/MPL/2.0/.

// +build solaris

package main

import (
	"os/exec"
	"strconv"
)

func memorySize() (int64, error) {
	cmd := exec.Command("prtconf", "-m")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return 0, err
	}

	mb, err := strconv.ParseInt(string(out), 10, 64)
	if err != nil {
		return 0, err
	}
	return mb * 1024 * 1024, nil
}
